#!/usr/bin/env bash

set -euo pipefail

REPO="${SAO_REPO:-code-rabi/simple-agent-orchastration}"
INSTALL_DIR="${SAO_INSTALL_DIR:-}"
VERSION_TAG="${SAO_VERSION_TAG:-}"

log() {
  printf '%s\n' "$*" >&2
}

fail() {
  log "error: $*"
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

detect_os() {
  case "$(uname -s)" in
    Linux) printf 'linux' ;;
    Darwin) printf 'darwin' ;;
    MINGW*|MSYS*|CYGWIN*) printf 'windows' ;;
    *) fail "unsupported operating system: $(uname -s)" ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) printf 'amd64' ;;
    arm64|aarch64) printf 'arm64' ;;
    *) fail "unsupported architecture: $(uname -m)" ;;
  esac
}

pick_install_dir() {
  if [ -n "${INSTALL_DIR}" ]; then
    printf '%s' "${INSTALL_DIR}"
    return
  fi

  if [ -w "/usr/local/bin" ]; then
    printf '/usr/local/bin'
    return
  fi

  printf '%s/.local/bin' "${HOME}"
}

latest_tag() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n 1
}

asset_ext() {
  case "$1" in
    windows) printf 'zip' ;;
    *) printf 'tar.gz' ;;
  esac
}

extract_archive() {
  archive="$1"
  target_dir="$2"
  case "$archive" in
    *.tar.gz) tar -xzf "$archive" -C "$target_dir" ;;
    *.zip)
      if command -v unzip >/dev/null 2>&1; then
        unzip -q "$archive" -d "$target_dir"
      else
        bsdtar -xf "$archive" -C "$target_dir"
      fi
      ;;
    *) fail "unsupported archive format: $archive" ;;
  esac
}

need_cmd curl
need_cmd uname
need_cmd mktemp
need_cmd tar

os="$(detect_os)"
arch="$(detect_arch)"
ext="$(asset_ext "$os")"
tag="${VERSION_TAG}"

if [ -z "${tag}" ]; then
  tag="$(latest_tag)"
fi

[ -n "${tag}" ] || fail "could not determine release tag"

asset="sao-${os}-${arch}.${ext}"
download_url="https://github.com/${REPO}/releases/download/${tag}/${asset}"
install_dir="$(pick_install_dir)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

mkdir -p "${install_dir}"

log "installing sao from ${tag}"
log "downloading ${download_url}"
curl -fsSL "${download_url}" -o "${tmp_dir}/${asset}"

extract_dir="${tmp_dir}/extract"
mkdir -p "${extract_dir}"
extract_archive "${tmp_dir}/${asset}" "${extract_dir}"

binary_name="sao"
if [ "${os}" = "windows" ]; then
  binary_name="sao.exe"
fi

[ -f "${extract_dir}/${binary_name}" ] || fail "archive did not contain ${binary_name}"
install -m 0755 "${extract_dir}/${binary_name}" "${install_dir}/${binary_name}"

log "installed ${install_dir}/${binary_name}"
case ":${PATH}:" in
  *:"${install_dir}":*) ;;
  *)
    log "note: ${install_dir} is not currently on PATH"
    ;;
esac
