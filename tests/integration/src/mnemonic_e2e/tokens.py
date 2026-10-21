"""Search tokens that only one pattern contains, as the OpenAI stub sees them.

The stub hashes every distinct word of an embedding input to one dimension
(FNV-1a 32 modulo the dimension count). A search for a token therefore matches
every pattern holding a word that hashes to the token's dimension; a pool hands
out only tokens whose dimension no shared word and no earlier token uses.
"""

import re
import secrets
import threading

from mnemonic_e2e.db import DIMENSIONS

_FNV_OFFSET = 0x811C9DC5
_FNV_PRIME = 0x01000193
_WORD = re.compile(r"[^\W_]+")


def words(text: str) -> list[str]:
    """Split text into lowercase runs of letters and digits, as the stub does."""
    return _WORD.findall(text.lower())


def stub_dimension(word: str) -> int:
    """The embedding dimension the stub hashes word to."""
    h = _FNV_OFFSET
    for byte in word.encode():
        h = ((h ^ byte) * _FNV_PRIME) & 0xFFFFFFFF
    return h % DIMENSIONS


class TokenPool:
    def __init__(self, shared_text: str) -> None:
        """shared_text holds every word the test patterns have in common."""
        self._taken = {stub_dimension(word) for word in words(shared_text)}
        self._lock = threading.Lock()

    def new(self) -> str:
        """Return "tok" plus 12 random hex digits on a dimension nothing else uses."""
        with self._lock:
            while True:
                token = "tok" + secrets.token_hex(6)
                dimension = stub_dimension(token)
                if dimension not in self._taken:
                    self._taken.add(dimension)
                    return token
