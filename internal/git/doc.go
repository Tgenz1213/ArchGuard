/*
Package git asks the git command line which files changed and what changed in
them: staged, uncommitted, every tracked file, or everything since a ref.

Every call runs git under the caller's context, so a cancelled run stops git
too and reports the cancellation, not git's exit status.

It does not choose which of these a check uses; package cli and package
analysis do.
*/
package git
