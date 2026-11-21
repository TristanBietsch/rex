#!/bin/sh
# Refuses a release whose commit is not reachable from origin/master.
#
# Usage: tag_on_master.sh COMMIT [TAG]
#
# COMMIT is the commit the tag points at ($GITHUB_SHA in the release
# workflow). TAG is only used in messages. Needs a full-history checkout with
# the origin/master ref present. Exit 0: on master. Exit 1: refused. Exit 2:
# bad usage.
set -eu

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	echo "usage: tag_on_master.sh COMMIT [TAG]" >&2
	exit 2
fi
commit=$1
tag=${2:-$1}

if ! git rev-parse --verify --quiet "refs/remotes/origin/master^{commit}" >/dev/null; then
	echo "error: origin/master not found; fetch full history before releasing $tag" >&2
	exit 1
fi

if ! git merge-base --is-ancestor "$commit" refs/remotes/origin/master; then
	echo "error: refusing release $tag: commit $commit is not on master" >&2
	exit 1
fi
