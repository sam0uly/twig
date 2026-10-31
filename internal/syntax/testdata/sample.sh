#!/bin/sh
# A shell script using a here-document; the body reads as one literal.
cat <<SQL
SELECT id, name
FROM users
WHERE active = 1;
SQL
echo "done"