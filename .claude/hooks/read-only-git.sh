#!/bin/bash
# Lets an agent run only git diff, log, show, status, grep or ls-files, with no
# shell operators, --output or grep's -O pager. Anything it can't parse is refused.
cmd=$(sed -nE 's/.*"command"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p')
if [[ $cmd =~ ^git\ (diff|log|show|status|grep|ls-files)(\ [^\;\&\|\`\$\<\>\\]*)?$ &&
  $cmd != *--output* && $cmd != *--op* && ! $cmd =~ \ -[[:alnum:]]*O ]]; then
  exit 0
fi
echo "Refused: this agent may only run git diff, log, show, status, grep or ls-files, one command at a time, with no cd." >&2
exit 2
