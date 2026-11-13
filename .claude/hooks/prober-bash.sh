#!/bin/bash
# Allowlist for the prober's Bash: single commands only, no shell operators.
cmd=$(sed -nE 's/.*"command"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p')
args='(\ [^\;\&\|\`\$\<\>\\]*)?$'
outpath='\ --?o[=\ ]+(/|[A-Za-z]:|[^\ ]*\.\.)'
writes='\ -(C|coverprofile|cpuprofile|memprofile|blockprofile|mutexprofile|trace|outputdir)([=\ ]|$)'
if [[ $cmd =~ ^go\ (build|test|vet|run|list)$args &&
  $cmd != *-exec* && $cmd != *-toolexec* && $cmd != *-overlay* &&
  ! $cmd =~ $outpath && ! $cmd =~ $writes ]]; then
  exit 0
fi
if [[ $cmd =~ ^golangci-lint\ run$args && $cmd != *--fix* ]]; then
  exit 0
fi
if [[ $cmd =~ ^git\ (diff|log|show|status|grep|ls-files|apply|restore)$args &&
  $cmd != *--output* && $cmd != *--op* && $cmd != *--unsafe-paths* &&
  $cmd != *--directory* && ! $cmd =~ \ -[[:alnum:]]*O && ! $cmd =~ $writes ]]; then
  exit 0
fi
if [[ $cmd == 'git reset --hard HEAD' || $cmd == 'git clean -fd' ]]; then
  exit 0
fi
if [[ $cmd =~ ^git\ checkout\ --detach\ (refs/review/pr-[0-9]+|[0-9a-f]{7,40})$ ]]; then
  exit 0
fi
if [[ $cmd =~ ^gh\ (pr\ (view|diff)|issue\ view)$args ]]; then
  exit 0
fi
echo "Refused: the prober may only run go build, test, vet, run or list, golangci-lint run, git diff, log, show, status, grep, ls-files, apply or restore, gh pr view or diff, or gh issue view, one command at a time." >&2
exit 2
