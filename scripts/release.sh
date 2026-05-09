#!/usr/bin/env bash
set -euo pipefail

PROJECT_NAME="glaz"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${ROOT_DIR}/dist"
STAGE_DIR="${DIST_DIR}/.stage"

VERSION="${1:-}"
if [[ -z "${VERSION}" ]]; then
  VERSION="v0.1.0"
fi

OS_NAME="linux"
ARCH_NAME="amd64"

echo "==> Preparing release artifacts for ${PROJECT_NAME} ${VERSION}"
rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}" "${STAGE_DIR}"

build_bundle() {
  local distro="$1"
  local bundle_dir="${STAGE_DIR}/${PROJECT_NAME}-${VERSION}-${distro}-${ARCH_NAME}"
  local bundle_name="${PROJECT_NAME}-${VERSION}-${distro}-${ARCH_NAME}.tar.gz"

  mkdir -p "${bundle_dir}"

  echo "==> Building binary for ${distro}"
  CGO_ENABLED=0 GOOS="${OS_NAME}" GOARCH="${ARCH_NAME}" go build -o "${bundle_dir}/${PROJECT_NAME}" .

  cp "${ROOT_DIR}/README.md" "${bundle_dir}/README.md"
  cp "${ROOT_DIR}/LICENSE" "${bundle_dir}/LICENSE"

  cat > "${bundle_dir}/RELEASE.txt" <<EOF
${PROJECT_NAME} ${VERSION}
Target distro: ${distro}
Platform: ${OS_NAME}/${ARCH_NAME}

Run:
  chmod +x ./glaz
  ./glaz -config glaz.json
EOF

  tar -C "${STAGE_DIR}" -czf "${DIST_DIR}/${bundle_name}" "$(basename "${bundle_dir}")"
}

build_bundle "ubuntu"
build_bundle "fedora"

echo "==> Building source archive"
SOURCE_NAME="${PROJECT_NAME}-${VERSION}-source.tar.gz"
tar \
  --exclude=".git" \
  --exclude="dist" \
  --exclude="${PROJECT_NAME}" \
  -C "${ROOT_DIR}" \
  -czf "${DIST_DIR}/${SOURCE_NAME}" \
  .

echo "==> Writing checksums"
(
  cd "${DIST_DIR}"
  sha256sum ./*.tar.gz > checksums.txt
)

cat > "${DIST_DIR}/RELEASE_UPLOAD_NOTES.md" <<EOF
# Release ${VERSION}

- ${PROJECT_NAME}-${VERSION}-ubuntu-${ARCH_NAME}.tar.gz
- ${PROJECT_NAME}-${VERSION}-fedora-${ARCH_NAME}.tar.gz
- ${PROJECT_NAME}-${VERSION}-source.tar.gz
- checksums.txt

To verify:
\`\`\`bash
tar -xzf ${PROJECT_NAME}-${VERSION}-ubuntu-${ARCH_NAME}.tar.gz
cd ${PROJECT_NAME}-${VERSION}-ubuntu-${ARCH_NAME}
chmod +x ./glaz
./glaz -config glaz.json
\`\`\`
EOF

rm -rf "${STAGE_DIR}"
echo "==> Done. Artifacts are in ${DIST_DIR}"
