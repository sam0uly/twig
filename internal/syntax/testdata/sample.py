"""A module docstring that opens on one line.

and closes on another.
"""

import os


class Thing:
    """A short docstring."""

    def method(self, count=3):
        if count is None:
            return False
        # a comment with a quote: " and an apostrophe: '
        total = 0x1F + 3.5e2
        text = 'single # not a comment'
        return f"{text} {total}"
