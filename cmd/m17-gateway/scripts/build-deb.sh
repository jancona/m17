#!/bin/bash

set -e

VERSION=${1:-1.0.0}
PACKAGE_NAME="m17-gateway"
ARCH="arm64"

echo "Building .deb package for ${PACKAGE_NAME} version ${VERSION} (${ARCH})"

# Create build directory
BUILD_DIR="build/${PACKAGE_NAME}_${VERSION}_${ARCH}"
mkdir -p "${BUILD_DIR}"

# Create directory structure
mkdir -p "${BUILD_DIR}/opt/m17/m17-gateway"
mkdir -p "${BUILD_DIR}/etc/systemd/system"
mkdir -p "${BUILD_DIR}/DEBIAN"

# Copy binary
cp "${PACKAGE_NAME}" "${BUILD_DIR}/opt/m17/m17-gateway/"

# Copy configuration file
cp "${PACKAGE_NAME}.ini.sample" "${BUILD_DIR}/etc/"

# Copy systemd service file
cp "${PACKAGE_NAME}.service" "${BUILD_DIR}/etc/systemd/system/"

# Copy control file and replace version
sed "s/VERSION_PLACEHOLDER/${VERSION}/g" debian/control > "${BUILD_DIR}/DEBIAN/control"

# Copy debian scripts
cp debian/postinst "${BUILD_DIR}/DEBIAN/"
cp debian/prerm "${BUILD_DIR}/DEBIAN/"
cp debian/postrm "${BUILD_DIR}/DEBIAN/"

# Make scripts executable
chmod +x "${BUILD_DIR}/DEBIAN/postinst"
chmod +x "${BUILD_DIR}/DEBIAN/prerm"
chmod +x "${BUILD_DIR}/DEBIAN/postrm"

# Build the package
dpkg-deb --build "${BUILD_DIR}"

echo "Package built: ${BUILD_DIR}.deb"
