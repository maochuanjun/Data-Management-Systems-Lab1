"""Public launcher contract: project-only config, precedence, no shell execution."""
# ruff: noqa: PT027

import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).resolve().parents[2] / "scripts/with-local-mongo.py"
spec = importlib.util.spec_from_file_location("project_config", SCRIPT)
loader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(loader)


class ProjectConfigTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.config = self.root / ".env"

    def test_all_project_settings_and_test_connection_are_loaded(self):
        self.config.write_text(
            "MONGO_URI='mongodb://project:27017/?directConnection=true'\n"
            "MONGO_DB=project_db\nPORT=8091\nCOOKIE_SECURE=true\n"
        )
        env = loader.command_environment(self.config, {})
        assert env["PORT"] == "8091"
        assert env["MONGO_DB"] == "project_db"
        assert env["COOKIE_SECURE"] == "true"
        assert env["INTEGRATION_MONGO_URI"] == env["MONGO_URI"]

    def test_explicit_environment_wins_and_is_not_mutated(self):
        self.config.write_text("MONGO_URI=mongodb://project\nPORT=8091\n")
        inherited = {
            "MONGO_URI": "mongodb://override",
            "PORT": "8092",
            "INTEGRATION_MONGO_URI": "mongodb://test-only",
        }
        before = dict(inherited)
        env = loader.command_environment(self.config, inherited)
        assert env["MONGO_URI"] == "mongodb://override"
        assert env["PORT"] == "8092"
        assert env["INTEGRATION_MONGO_URI"] == "mongodb://test-only"
        assert inherited == before

    def test_external_config_never_substitutes_missing_project_configuration(self):
        external = self.root / "external.env"
        external.write_text("MONGO_URI=mongodb://external\n")
        with self.assertRaisesRegex(ValueError, "MONGO_URI"):
            loader.command_environment(self.config, {"LOCAL_MONGO_ENV": str(external)})
        assert (
            loader.command_environment(self.config, {"MONGO_URI": "mongodb://ci"})[
                "MONGO_URI"
            ]
            == "mongodb://ci"
        )

    def test_bad_configuration_does_not_echo_secret_values(self):
        for content in ["MONGO_URI='fake-secret-value\n", "fake-secret-value\n"]:
            self.config.write_text(content)
            with self.assertRaises(ValueError) as caught:
                loader.command_environment(self.config, {})
            assert "fake-secret-value" not in str(caught.exception)

    def test_values_are_data_and_never_executed_by_a_shell(self):
        marker = self.root / "must-not-exist"
        self.config.write_text(
            f"MONGO_URI='mongodb://project'\nLITERAL='$(touch {marker})'\n"
        )
        env = loader.command_environment(self.config, {})
        assert env["LITERAL"] == f"$(touch {marker})"
        assert not marker.exists()

    def test_launcher_resolves_project_directory_and_passes_configuration(self):
        scripts = self.root / "project/scripts"
        scripts.mkdir(parents=True)
        copied = scripts / SCRIPT.name
        shutil.copyfile(SCRIPT, copied)
        (scripts.parent / ".env").write_text("MONGO_URI=mongodb://project\nPORT=8091\n")
        inherited = {
            key: value
            for key, value in os.environ.items()
            if key not in {"MONGO_URI", "PORT", "INTEGRATION_MONGO_URI"}
        }
        result = subprocess.run(
            [
                sys.executable,
                str(copied),
                sys.executable,
                "-c",
                "import json,os; print(json.dumps([os.getenv('PORT'),os.getenv('INTEGRATION_MONGO_URI')]))",
            ],
            cwd=self.root,
            env=inherited,
            capture_output=True,
            text=True,
            check=True,
        )
        assert json.loads(result.stdout) == ["8091", "mongodb://project"]
        assert result.stderr == ""


if __name__ == "__main__":
    unittest.main()
