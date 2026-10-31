from contextlib import redirect_stdout
import copy
import io
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import MagicMock, patch

import staging_deployment as staging


def service(amd_env="sandbox"):
    revision = staging.SERVICE + "-00001-test"
    return {
        "metadata": {"name": staging.SERVICE},
        "spec": {"template": {"spec": {
            "serviceAccountName": f"{staging.SERVICE}@{staging.PROJECT}.iam.gserviceaccount.com",
            "containers": [{"image": "old-image", "env": [
                {"name": "AMD_ENV", "value": amd_env},
                {"name": "ACUITY_PRODUCT_URL", "value": "https://acuity-portal-api-example.a.run.app"},
            ] + [{"name": key, "valueFrom": {"secretKeyRef": {"name": value, "key": "1"}}}
                 for key, value in staging.SECRETS.items()]}],
        }}},
        "status": {
            "url": "https://abita-middleware-sandbox-example.run.app",
            "latestCreatedRevisionName": revision,
            "latestReadyRevisionName": revision,
            "conditions": [{"type": "Ready", "status": "True"}],
            "traffic": [{"revisionName": revision, "percent": 100}],
        },
    }


class StagingDeploymentTest(unittest.TestCase):
    def test_rejects_production_config_and_mutable_secrets(self):
        for value in ["prod", "dev", ""]:
            with self.subTest(amd_env=value), self.assertRaises(RuntimeError):
                staging.validate(service(value))
        for key, value in [("name", "advancedmd-password"), ("key", "latest")]:
            candidate = service()
            candidate["spec"]["template"]["spec"]["containers"][0]["env"][2]["valueFrom"]["secretKeyRef"][key] = value
            with self.subTest(field=key), self.assertRaises(RuntimeError):
                staging.validate(candidate)

    def test_rejects_a_sandbox_without_an_https_product_url(self):
        for value in [None, "", "http://acuity-portal-api-example.a.run.app", "https://acuity-portal-api-example.a.run.app/v1"]:
            candidate = service()
            env = candidate["spec"]["template"]["spec"]["containers"][0]["env"]
            if value is None:
                del env[1]
            else:
                env[1]["value"] = value
            with self.subTest(url=value), self.assertRaises(RuntimeError):
                staging.validate(candidate)

    def test_rejects_ready_service_with_pending_revision_or_split_traffic(self):
        for change in [{"latestCreatedRevisionName": "pending"}, {
            "traffic": [{"revisionName": "old-revision", "percent": 100}],
        }]:
            candidate = service()
            candidate["status"].update(change)
            with self.assertRaises(RuntimeError):
                staging.validate(candidate)

    def test_image_update_must_preserve_runtime_configuration(self):
        before = service("sandbox")
        after = service("sandbox")
        after["spec"]["template"]["spec"]["containers"][0]["image"] = "new-image"
        self.assertEqual(staging.runtime_spec(before), staging.runtime_spec(after))
        changed = copy.deepcopy(after)
        changed["spec"]["template"]["spec"]["containerConcurrency"] = 100
        self.assertNotEqual(staging.runtime_spec(before), staging.runtime_spec(changed))
        changed = copy.deepcopy(after)
        changed["spec"]["template"]["spec"]["containers"][0]["env"][2]["valueFrom"]["secretKeyRef"]["key"] = "2"
        self.assertNotEqual(staging.runtime_spec(before), staging.runtime_spec(changed))
        self.assertNotEqual(staging.runtime_spec(before), staging.runtime_spec(service("dev")))

    @patch.object(staging, "cloud", return_value="test-token")
    @patch.object(staging, "request")
    def test_smoke_proves_auth_without_provider_requests(self, request, cloud):
        request.side_effect = [(200, b""), (200, b""), (401, b""), (200, json.dumps({
            "status": "error", "message": "Invalid JSON body", "appointments": None, "matches": None,
        }).encode())]
        result = staging.smoke(service())
        self.assertEqual(result["authenticated_request_validation"], "passed_without_provider_call")
        self.assertEqual(request.call_args.kwargs, {"body": b"{", "token": "test-token"})

    @patch.object(staging, "cloud", return_value="test-token")
    @patch.object(staging, "request")
    def test_http_200_provider_failure_is_not_authentication_proof(self, request, cloud):
        request.side_effect = [(200, b""), (200, b""), (401, b""),
                               (200, b'{"status":"error","message":"provider failure"}')]
        with self.assertRaises(RuntimeError):
            staging.smoke(service())


SHA = "a" * 40
DIGEST = "sha256:" + "b" * 64
SANDBOX_IMAGE = staging.REPOSITORY + "@" + DIGEST
PRODUCTION_IMAGE = f"{staging.REGION}-docker.pkg.dev/{staging.PROJECT}/services/abita-middleware@{DIGEST}"


class ReleaseDeploymentTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        previous = os.getcwd()
        os.chdir(directory.name)
        self.addCleanup(os.chdir, previous)

    def deploy(self, production_image, ref="refs/heads/main", before_env="sandbox", after_env="sandbox"):
        before = service(before_env)
        after = service(after_env)
        revision = staging.SERVICE + "-00002-test"
        after["spec"]["template"]["spec"]["containers"][0]["image"] = SANDBOX_IMAGE
        after["metadata"]["labels"] = {"source-sha": SHA}
        after["status"].update({
            "latestCreatedRevisionName": revision, "latestReadyRevisionName": revision,
            "traffic": [{"revisionName": revision, "percent": 100}],
        })
        describe = MagicMock(side_effect=[before, after, after])
        cloud = MagicMock(side_effect=lambda *args: SANDBOX_IMAGE if args[:2] == ("run", "revisions")
                          else "test-token")
        responses = [(200, b""), (200, b""), (401, b""),
                     (200, json.dumps({"status": "error", "message": "Invalid JSON body"}).encode())]
        environment = {"GITHUB_SHA": SHA, "STAGING_IMAGE": SANDBOX_IMAGE,
                       "PRODUCTION_IMAGE": production_image, "GITHUB_REF": ref}
        error = None
        with patch.dict(os.environ, environment), \
                patch.object(sys, "argv", ["staging_deployment.py", "--deploy"]), \
                patch.object(staging, "describe", describe), \
                patch.object(staging, "cloud", cloud), \
                patch.object(staging, "request", side_effect=responses), \
                redirect_stdout(io.StringIO()):
            try:
                staging.main()
            except RuntimeError as raised:
                error = raised
        receipt = json.loads(Path("staging-deployment.json").read_text())
        self.cloud = cloud
        return error, receipt, describe

    def update_calls(self):
        return [call.args for call in self.cloud.call_args_list if call.args[:3] == ("run", "services", "update")]

    def test_deploy_updates_only_the_image(self):
        error, receipt, _ = self.deploy(PRODUCTION_IMAGE)
        self.assertIsNone(error)
        self.assertEqual(receipt["verification"], "deployment_ready_and_api_auth_verified")
        updates = self.update_calls()
        self.assertEqual(len(updates), 1)
        self.assertEqual(updates[0][updates[0].index("--image") + 1], SANDBOX_IMAGE)
        self.assertNotIn("--update-env-vars", updates[0])

    def test_deploy_refuses_a_sandbox_still_on_the_legacy_environment(self):
        error, receipt, _ = self.deploy(PRODUCTION_IMAGE, before_env="dev")
        self.assertIsInstance(error, RuntimeError)
        self.assertEqual(receipt["verification"], "failed")
        self.assertEqual(self.update_calls(), [])

    def test_release_deploy_records_matching_production_and_sandbox_digests(self):
        error, receipt, _ = self.deploy(PRODUCTION_IMAGE)
        self.assertIsNone(error)
        self.assertEqual(receipt["verification"], "deployment_ready_and_api_auth_verified")
        self.assertEqual(receipt["image"], SANDBOX_IMAGE)
        self.assertEqual(receipt["production_image"], PRODUCTION_IMAGE)

    def test_release_deploy_rejects_a_different_production_image_before_any_cloud_call(self):
        for production_image in [
            PRODUCTION_IMAGE.replace("b" * 64, "c" * 64),
            PRODUCTION_IMAGE.replace("@" + DIGEST, ":" + SHA),
            SANDBOX_IMAGE,
        ]:
            with self.subTest(production_image=production_image):
                error, receipt, describe = self.deploy(production_image)
                self.assertIsInstance(error, RuntimeError)
                self.assertEqual(receipt["verification"], "failed")
                self.assertEqual(receipt["production_image"], production_image)
                describe.assert_not_called()

    def test_release_tag_deploy_requires_the_production_image_before_any_cloud_call(self):
        error, receipt, describe = self.deploy("", ref="refs/tags/v9.9.9")
        self.assertIsInstance(error, RuntimeError)
        self.assertEqual(receipt["verification"], "failed")
        describe.assert_not_called()


if __name__ == "__main__":
    unittest.main()
