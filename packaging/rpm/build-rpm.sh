#!/bin/bash
# Build script for creating RPM packages

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "${SCRIPT_DIR}/../.." && pwd )"

# Parse arguments
VERSION="${1:-dev}"
GIT_COMMIT="${2:-$(git rev-parse HEAD 2>/dev/null || echo 'unknown')}"
BUILD_NUMBER="${3:-1}"

echo -e "${GREEN}Building DNS Monitor RPM package${NC}"
echo "Version: ${VERSION}"
echo "Commit: ${GIT_COMMIT}"
echo "Build: ${BUILD_NUMBER}"

# Check for required tools
for tool in rpmbuild go git; do
    if ! command -v $tool &> /dev/null; then
        echo -e "${RED}Error: $tool is not installed${NC}"
        exit 1
    fi
done

# Setup RPM build tree
RPM_BUILD_ROOT="${HOME}/rpmbuild"
mkdir -p "${RPM_BUILD_ROOT}"/{BUILD,RPMS,SOURCES,SPECS,SRPMS}

# Create source tarball
cd "${PROJECT_ROOT}"
TAR_NAME="dns-monitor-${VERSION}"
TAR_FILE="${TAR_NAME}.tar.gz"

echo -e "${YELLOW}Creating source tarball...${NC}"
# Create temporary directory with proper naming
TEMP_DIR=$(mktemp -d)
DEST_DIR="${TEMP_DIR}/${TAR_NAME}"
mkdir -p "${DEST_DIR}"

# Copy source files
cp -r . "${DEST_DIR}/"
# Clean unnecessary files
rm -rf "${DEST_DIR}/.git" "${DEST_DIR}/.github" "${DEST_DIR}/rpmbuild" "${DEST_DIR}/.gitignore"

# Create tarball
cd "${TEMP_DIR}"
tar -czf "${RPM_BUILD_ROOT}/SOURCES/${TAR_FILE}" "${TAR_NAME}"
cd -
rm -rf "${TEMP_DIR}"

echo -e "${YELLOW}Copying spec file...${NC}"
# Copy spec file
cp "${SCRIPT_DIR}/dns-monitor.spec" "${RPM_BUILD_ROOT}/SPECS/"

# Build RPM
echo -e "${YELLOW}Building RPM package...${NC}"
rpmbuild -ba \
    --define "_version ${VERSION}" \
    --define "_commit ${GIT_COMMIT}" \
    --define "_arch $(uname -m)" \
    "${RPM_BUILD_ROOT}/SPECS/dns-monitor.spec"

# Copy built RPMs to output directory
OUTPUT_DIR="${PROJECT_ROOT}/dist/rpm"
mkdir -p "${OUTPUT_DIR}"

echo -e "${YELLOW}Copying RPM packages to ${OUTPUT_DIR}...${NC}"
find "${RPM_BUILD_ROOT}/RPMS" -name "*.rpm" -exec cp {} "${OUTPUT_DIR}/" \;
find "${RPM_BUILD_ROOT}/SRPMS" -name "*.rpm" -exec cp {} "${OUTPUT_DIR}/" \;

# List built packages
echo -e "${GREEN}Successfully built RPM packages:${NC}"
ls -la "${OUTPUT_DIR}"/*.rpm

# Generate repository metadata (optional)
if command -v createrepo &> /dev/null; then
    echo -e "${YELLOW}Generating repository metadata...${NC}"
    createrepo "${OUTPUT_DIR}"
fi

echo -e "${GREEN}RPM build complete!${NC}"
echo -e "Packages are available in: ${OUTPUT_DIR}"

# Cleanup
echo -e "${YELLOW}Cleaning up...${NC}"
rm -f "${RPM_BUILD_ROOT}/SOURCES/${TAR_FILE}"