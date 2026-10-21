"""The suite is black-box: nothing in this project may reference service source."""

import re
from pathlib import Path

PROJECT = Path(__file__).resolve().parent.parent
SRC_REFERENCE = re.compile(r"(\.\./src\b|\bsrc/internal\b|\bsrc/cmd\b|\bsrc/migrations\b)")


def test_project_files_do_not_reference_service_source() -> None:
    offenders = [
        str(path.relative_to(PROJECT))
        for folder in ("src", "tests")
        for path in (PROJECT / folder).rglob("*.py")
        if path.name != Path(__file__).name and SRC_REFERENCE.search(path.read_text())
    ]

    assert offenders == []
