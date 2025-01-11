#!/usr/bin/env bash
set -o errexit
set -o pipefail
set -o nounset

if [ "$#" -gt 1 ] ; then
  echo "$0 supports exactly 1 or no arguments"
  exit 1
fi

root_dir="$(git rev-parse --show-toplevel)"

cd "${root_dir}"

if [ "$#" -eq 1 ] ; then
  image_dir="${1}"
  if ! [ -d "${image_dir}" ] ; then
    echo "${image_dir} is not a directory (path is relative to git root)"
    exit 1
  fi
  git_ls_tree="$(git ls-tree --full-tree HEAD -- "${image_dir}")"
  if [ -z "${git_ls_tree}" ] ; then
    echo "${image_dir} exists, but it is not checked in git (path is relative to git root)"
    exit 1
  fi
  image_tag="$(printf "%s" "${git_ls_tree}" | sed 's/^[0-7]\{6\} tree \([0-9a-f]\{40\}\).*/\1/')"
else
  image_dir="${root_dir}"
  git_tag="$(git name-rev --name-only --tags HEAD)"
  if printf "%s" "${git_tag}" | grep -q -E '^[v]?[0-9]+\.[0-9]+\.[0-9]+.*$' ; then
    git_tag="$(git tag --sort tag --points-at "${git_tag}")"
    image_tag="$(printf "%s" "${git_tag}" | sed 's/^[v]*/v/' | uniq)"
  else
    image_tag="$(git rev-parse --short HEAD)"
    if [ -z "${WITHOUT_SUFFIX+x}" ] ; then
      if ! git merge-base --is-ancestor "$(git rev-parse HEAD)" origin/main ; then
        image_tag="${image_tag}-dev"
      fi
    fi
  fi
fi

if [ -z "${WITHOUT_SUFFIX+x}" ] ; then
  if [ "$(git status --porcelain "${image_dir}" | wc -l)" -gt 0 ] ; then
    image_tag="${image_tag}-wip"
  fi
fi

printf "%s" "${image_tag}"
