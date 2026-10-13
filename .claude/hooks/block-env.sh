#!/bin/bash
# Refuses a Bash command that names a .env file. The Read deny rules in
# settings.json cover the file tools and commands like cat, but not a script
# such as python3 -c "open('.env')". It checks the whole hook input, so a
# command it can't parse is still refused.
if grep -qE '(^|[^[:alnum:]_])\.env([^[:alnum:]_]|$)'; then
  echo "Refused: .env holds API keys, and Claude may not read it." >&2
  exit 2
fi
exit 0
