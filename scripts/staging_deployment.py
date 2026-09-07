"""Update only the existing sandbox; emit a receipt without provider data."""

import argparse
import copy
import json
import os
from pathlib import Path
import re
import subprocess
import urllib.error
import urllib.request

PROJECT = "acuity-health-prod"
REGION = "us-east4"
SERVICE = "abita-middleware-sandbox"
REPOSITORY = f"{REGION}-docker.pkg.dev/{PROJECT}/middleware-sandbox/abita-middleware"
SECRETS = {
    "ADVANCEDMD_USERNAME": "advancedmd-sandbox-username",
    "ADVANCEDMD_PASSWORD": "advancedmd-sandbox-password",
    "ADVANCEDMD_OFFICE_KEY": "advancedmd-sandbox-office-key",
    "ADVANCEDMD_APP_NAME": "advancedmd-sandbox-app-name",
    "API_SECRET": "middleware-sandbox-api-secret",
    "BOOKING_TOKEN_SECRET": "sandbox-booking-token-secret",
}


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def cloud(*args):
    return subprocess.check_output(
        ["gcloud", *args, "--project", PROJECT, "--quiet"], text=True
    ).strip()


def describe():
    return json.loads(cloud("run", "services", "describe", SERVICE,
                            "--region", REGION, "--format=json"))


def validate(service):
    require(service["metadata"]["name"] == SERVICE, "Wrong Cloud Run service")
    spec = service["spec"]["template"]["spec"]
    require(spec["serviceAccountName"] == f"{SERVICE}@{PROJECT}.iam.gserviceaccount.com",
            "Sandbox runtime identity changed")
    require(len(spec["containers"]) == 1, "Unexpected sandbox containers")
    env = {item["name"]: item for item in spec["containers"][0]["env"]}
    require(env.get("AMD_ENV", {}).get("value") == "dev", "Sandbox must use AMD_ENV=dev")
    require(env.get("ALLOW_RAW_SLOT_BOOKING", {}).get("value") == "false",
            "Raw slot booking must remain disabled")
    for variable, name in SECRETS.items():
        ref = env.get(variable, {}).get("valueFrom", {}).get("secretKeyRef", {})
        require(ref.get("name") == name and re.fullmatch(r"[1-9][0-9]*", ref.get("key", "")),
                f"Invalid sandbox secret binding: {variable}")
    require(service["status"]["latestCreatedRevisionName"] == service["status"]["latestReadyRevisionName"],
            "Latest sandbox revision is not ready")
    require(any(c["type"] == "Ready" and c["status"] == "True"
                for c in service["status"]["conditions"]), "Sandbox service is not ready")
    traffic = service["status"]["traffic"]
    require(sum(t.get("percent", 0) for t in traffic
                if t.get("revisionName") == service["status"]["latestReadyRevisionName"]) == 100,
            "Sandbox traffic is not entirely on the ready revision")
    url = service["status"]["url"]
    require(re.fullmatch(r"https://abita-middleware-sandbox-[a-z0-9.-]+\.run\.app", url),
            "Unexpected sandbox URL")
    return env


def runtime_spec(service):
    spec = copy.deepcopy(service["spec"]["template"]["spec"])
    spec["containers"][0].pop("image")
    return spec


def request(url, body=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    req = urllib.request.Request(url, data=body, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=60) as response:
            return response.status, response.read()
    except urllib.error.HTTPError as error:
        return error.code, error.read()


def smoke(service):
    url = service["status"]["url"]
    checks = {}
    for path in ("/live", "/ready"):
        status, _ = request(url + path)
        require(status == 200, f"{path} readiness failed: HTTP {status}")
        checks[path] = "passed"
    # Invalid JSON is rejected before any provider call, even after API auth.
    status, _ = request(url + "/api/patient/resolve", body=b"{")
    require(status == 401, "Missing API token was not rejected")
    checks["unauthenticated_request"] = "rejected_401"
    env = validate(service)
    version = env["API_SECRET"]["valueFrom"]["secretKeyRef"]["key"]
    token = cloud("secrets", "versions", "access", version, "--secret", SECRETS["API_SECRET"])
    require(bool(token), "Sandbox API secret is empty")
    status, body = request(url + "/api/patient/resolve", body=b"{", token=token)
    require(status == 200 and json.loads(body) == {
        "status": "error", "message": "Invalid JSON body", "appointments": None,
    },
            "Sandbox API token did not reach request validation")
    checks["authenticated_request_validation"] = "passed_without_provider_call"
    return checks


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--deploy", action="store_true")
    args = parser.parse_args()
    sha = os.environ.get("GITHUB_SHA", "")
    image = os.environ.get("STAGING_IMAGE", "")
    receipt = {
        "source_sha": sha, "image": image, "service": SERVICE,
        "run_url": os.environ.get("GITHUB_SERVER_URL", "https://github.com") + "/" +
        os.environ.get("GITHUB_REPOSITORY", "Data-Buddies-Solutions/amd_middleware") +
        "/actions/runs/" + os.environ.get("GITHUB_RUN_ID", ""),
        "verification": "failed",
        "provider_verification": "pending: no verified live synthetic patient fixture",
    }
    try:
        require(re.fullmatch(r"[0-9a-f]{40}", sha), "Expected full Git source SHA")
        if args.deploy:
            require(re.fullmatch(re.escape(REPOSITORY) + r"@sha256:[0-9a-f]{64}", image),
                    "Expected immutable image in sandbox-only repository")
        before = describe()
        validate(before)
        receipt["previous_revision"] = before["status"]["latestReadyRevisionName"]
        receipt["previous_image"] = before["spec"]["template"]["spec"]["containers"][0]["image"]
        if args.deploy:
            # Image and provenance labels only; keep all runtime configuration.
            cloud("run", "services", "update", SERVICE, "--region", REGION,
                  "--image", image, "--update-labels", f"source-sha={sha}")
        after = describe()
        validate(after)
        receipt["revision"] = after["status"]["latestReadyRevisionName"]
        actual_image = after["spec"]["template"]["spec"]["containers"][0]["image"]
        require(actual_image == image, "Deployed image differs from requested digest")
        receipt["revision_image"] = cloud(
            "run", "revisions", "describe", receipt["revision"],
            "--region", REGION, "--format=value(status.imageDigest)",
        )
        require(receipt["revision_image"] == image, "Ready revision has a different image digest")
        if args.deploy:
            require(after["metadata"]["labels"].get("source-sha") == sha,
                    "Deployed source label does not match Git source")
        require(runtime_spec(before) == runtime_spec(after), "Sandbox runtime configuration changed")
        receipt["checks"] = smoke(after)
        require(describe()["status"]["latestReadyRevisionName"] == receipt["revision"],
                "Sandbox revision changed during smoke verification")
        receipt["verification"] = "deployment_ready_and_api_auth_verified"
    finally:
        rendered = json.dumps(receipt, indent=2) + "\n"
        Path("staging-deployment.json").write_text(rendered)
        if os.environ.get("GITHUB_STEP_SUMMARY"):
            with open(os.environ["GITHUB_STEP_SUMMARY"], "a") as summary:
                summary.write("## Sandbox deployment receipt\n\n```json\n" + rendered + "```\n")
        print(rendered)


if __name__ == "__main__":
    main()
