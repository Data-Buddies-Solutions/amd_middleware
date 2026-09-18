import copy
import json
import unittest
from unittest.mock import patch

import staging_deployment as staging


def service():
    revision = staging.SERVICE + "-00001-test"
    return {
        "metadata": {"name": staging.SERVICE},
        "spec": {"template": {"spec": {
            "serviceAccountName": f"{staging.SERVICE}@{staging.PROJECT}.iam.gserviceaccount.com",
            "containers": [{"image": "old-image", "env": [
                {"name": "AMD_ENV", "value": "dev"},
                {"name": "ALLOW_RAW_SLOT_BOOKING", "value": "false"},
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
        for field, value in [("AMD_ENV", "prod"), ("ALLOW_RAW_SLOT_BOOKING", "true")]:
            candidate = service()
            env = candidate["spec"]["template"]["spec"]["containers"][0]["env"]
            next(item for item in env if item["name"] == field)["value"] = value
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                staging.validate(candidate)
        for key, value in [("name", "advancedmd-password"), ("key", "latest")]:
            candidate = service()
            candidate["spec"]["template"]["spec"]["containers"][0]["env"][2]["valueFrom"]["secretKeyRef"][key] = value
            with self.subTest(field=key), self.assertRaises(RuntimeError):
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
        before = service()
        after = copy.deepcopy(before)
        after["spec"]["template"]["spec"]["containers"][0]["image"] = "new-image"
        self.assertEqual(staging.runtime_spec(before), staging.runtime_spec(after))
        after["spec"]["template"]["spec"]["containerConcurrency"] = 100
        self.assertNotEqual(staging.runtime_spec(before), staging.runtime_spec(after))

    @patch.object(staging, "cloud", return_value="test-token")
    @patch.object(staging, "request")
    def test_smoke_proves_auth_without_provider_requests(self, request, cloud):
        request.side_effect = [(200, b""), (200, b""), (401, b""), (200, json.dumps({
            "status": "error", "message": "Invalid JSON body", "appointments": None,
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


if __name__ == "__main__":
    unittest.main()
