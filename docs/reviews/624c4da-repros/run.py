#!/usr/bin/env python3
"""Run isolated review probes. Nonzero test exits reproduce a reported gap.

No tracked production/test source is changed. Java interruption requires the
existing compiled Spring fixture classes and Surefire classpath from acceptance.
"""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
CASES = {
    "copilot-burst": ("internal/adapters/outbound/copilot", "copilot", "TestReviewEventsBeforeSendReply"),
    "copilot-race": ("internal/adapters/outbound/copilot", "copilot", "TestReviewCatalogCancellationRace"),
    "installer-path": ("tools/setup", "installer", "TestReviewCanonicalPathSeparator"),
    "http-schema": ("internal/adapters/inbound/openai", "http", "TestReviewNullObfuscationContract"),
    "service-identity": ("cmd/harness-gateway", "service", "TestReviewServiceManagerIdentity"),
    "acceptance-interrupt": ("tools/acceptance", "interrupt", "TestReviewInterruptedFixtureOwnership"),
}


def prepare_java(work, env):
    reports = list((ROOT / "examples/spring-ai/target/surefire-reports").glob("TEST-*-failure.xml"))
    if not reports:
        raise RuntimeError("Run ordinary Spring AI fixture acceptance first to prepare Java classes/classpath")
    properties = ET.parse(reports[0]).getroot().find("properties")
    classpath = next(p.attrib["value"] for p in properties if p.attrib.get("name") == "java.class.path")
    (work / "classpath").write_text(classpath)
    source = work / "ReviewGate.java"
    source.write_text((HERE / "ReviewGate.java.txt").read_text())
    for args in [
        ["go", "build", "-o", str(work / "gateway"), "./cmd/harness-gateway"],
        ["go", "test", "-c", "-o", str(work / "codex-fixture"), "./internal/adapters/outbound/codex"],
        ["javac", "-cp", classpath, "-d", str(work), str(source)],
    ]:
        subprocess.run(args, cwd=ROOT, env=env, check=True)
    env["HG_REVIEW_INTERRUPT_ROOT"] = str(work)


def run(case):
    package, artifact, test = CASES[case]
    with tempfile.TemporaryDirectory(prefix="harness-gateway-egm-probe-") as scratch:
        work = Path(scratch)
        env = os.environ.copy()
        env.setdefault("GOCACHE", "/tmp/harness-gateway-review-go-cache")
        replace = {str(ROOT / package / "egm_review_test.go"): str(HERE / (artifact + "-probes.go.txt"))}
        if artifact == "copilot" and any(signature in (ROOT / package / "client.go").read_text() for signature in ("func() error, func() error, error)", "*cliBridge, func() error, error)")):
            # The remediation exposes transport close separately for pending writes.
            probe = work / "copilot-probes.go"
            probe.write_text((HERE / "copilot-probes.go.txt").read_text().replace("client,_,stop,err:=", "client,_,_,stop,err:="))
            replace[str(ROOT / package / "egm_review_test.go")] = str(probe)
        if case == "copilot-burst":
            original = ROOT / package / "copilot_test.go"
            source = original.read_text()
            hook = '\t\tcase "session.send":\n'
            if source.count(hook) != 1:
                raise RuntimeError("Fixture hook changed; inspect the probe before running on another revision")
            injection = hook + '''
            if mode == "review-event-burst" {
                for i := 0; i < 256; i++ {
                    if err := event(&sdk.AssistantMessageDeltaData{MessageID: "burst", DeltaContent: "x"}, nil); err != nil { return err }
                }
                if err := reply(map[string]string{"messageId": "fixture-user"}); err != nil { return err }
                if err := event(&sdk.SessionIdleData{}, nil); err != nil { return err }
                continue
            }
'''
            altered = work / "copilot_test.go"
            altered.write_text(source.replace(hook, injection))
            replace[str(original)] = str(altered)
        if case == "acceptance-interrupt":
            prepare_java(work, env)
        overlay = work / "overlay.json"
        overlay.write_text(json.dumps({"Replace": replace}))
        args = ["go", "test", "-overlay", str(overlay), "-count=1", "-timeout=2m", "-run", "^" + test + "$", "-v"]
        if case == "copilot-race":
            args.append("-race")
        args.append("./" + package)
        return subprocess.run(args, cwd=ROOT, env=env, check=False).returncode


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("case", choices=CASES)
    arguments = parser.parse_args()
    raise SystemExit(run(arguments.case))
