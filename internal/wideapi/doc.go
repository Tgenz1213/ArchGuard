/*
Package wideapi finds the exported API a branch changes in the packages that
many others import.

A change to an exported identifier of such a package reaches every package
that imports it, so the Wide API job of a pull request warns about one instead
of leaving it to a reviewer to notice. The warning never blocks a merge.

It does not decide whether a change is justified: .claude/rules/wide-packages.md
says what to ask, and the reviewer agent asks it.

Terms:

  - Reach: how many packages of the module import a package, directly or
    through another. It is counted on the pushed commit, not the working tree.
  - Wide package: one whose reach is at least the minimum the command passes.
  - Surface: a package's exported declarations as parsed Go, so a reformat, a
    renamed parameter or a changed unexported name is not a change, while an
    embedded type, an interface made unimplementable outside the package or
    the value of an iota constant is.
*/
package wideapi
