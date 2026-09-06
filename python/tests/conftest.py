"""Make the package and the shared helpers importable under pytest.

The suite is written against :mod:`unittest` so it also runs with a bare
interpreter (``python -m unittest discover -s tests``), which is the honest way
to test a package that claims zero required dependencies. This file only exists
so pytest finds the same modules.
"""

import sys
from pathlib import Path

_HERE = Path(__file__).resolve().parent
for path in (_HERE, _HERE.parent):
    if str(path) not in sys.path:
        sys.path.insert(0, str(path))
