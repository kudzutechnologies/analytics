import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('release', ROOT / 'tools/release-basicstation.py')
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseInputsTest(unittest.TestCase):
    def test_missing_inputs_fail_closed(self):
        with patch.dict('os.environ', {}, clear=True):
            with self.assertRaisesRegex(ValueError, 'missing release prerequisites'):
                release.inputs()

    def test_hardware_evidence_is_required_for_each_target(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'bin').mkdir()
            header = root / 'producer.h'
            header.write_text('// fixture, never used as production ABI evidence')
            sdk, evidence = {}, {}
            ref = 'a' * 40
            for name, prefix in release.TARGETS.items():
                (root / 'bin' / (prefix + '-g++')).touch()
                sdk[name] = dict(toolchain_root=str(root), sysroot=str(root),
                                 host_protoc=str(header), host_grpc_plugin=str(header),
                                 protobuf_version='fixture', grpc_version='fixture',
                                 runtime_dependencies='fixture')
                evidence[name] = dict(producer_ref=ref, passed=True, binary_sha256='b' * 64,
                                      checks={key: True for key in ['install', 'start', 'stop', 'upgrade', 'radio', 'minimum-memory', 'outage']})
            sdk_path, proof = root / 'sdk.json', root / 'evidence.json'
            sdk_path.write_text(json.dumps(sdk))
            proof.write_text(json.dumps(evidence))
            env = dict(BASICSTATION_SDK_MANIFEST=str(sdk_path), BASICSTATION_PRODUCER_HEADER=str(header),
                       BASICSTATION_PRODUCER_REF=ref, BASICSTATION_HARDWARE_EVIDENCE=str(proof))
            with patch.dict('os.environ', env, clear=True):
                release.inputs()
                evidence['kerlink']['checks']['outage'] = False
                proof.write_text(json.dumps(evidence))
                with self.assertRaisesRegex(ValueError, 'kerlink: incomplete hardware'):
                    release.inputs()


if __name__ == '__main__':
    unittest.main()
