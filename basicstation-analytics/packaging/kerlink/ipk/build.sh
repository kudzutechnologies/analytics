#!/bin/sh
# SPDX-License-Identifier: MIT
# Copyright 2026 Kudzu Technologies
OPKG_PKG_NAME="kudzu-bs-analytics"
VERSION="0.1.0"
ARCH="klkgw"
ASSETS_DIR="/assets/pkg"
BUILD_DIR=$(mktemp -d -t opkg_build_XXXXXXX)
OPKG_PKG_DIR="${BUILD_DIR}/package"

rm -rf ${OPKG_PKG_DIR}
mkdir -p ${OPKG_PKG_DIR}/CONTROL
cp ${ASSETS_DIR}/ipk/control ${OPKG_PKG_DIR}/CONTROL/control
cp -vr ${ASSETS_DIR}/etc ${OPKG_PKG_DIR}/etc
mkdir -p ${OPKG_PKG_DIR}/usr/bin
cp -v /assets/bin/basicstation-analytics ${OPKG_PKG_DIR}/usr/bin/basicstation-analytics

cat > ${OPKG_PKG_DIR}/CONTROL/postinst <<'EOF'
ln -sf ../init.d/kudzu-bs-analytics /etc/rcU.d/S53kudzu-bs-analytics
ln -sf ../init.d/kudzu-bs-analytics /etc/rcK.d/K53kudzu-bs-analytics
EOF
chmod 755 ${OPKG_PKG_DIR}/CONTROL/postinst

cat > ${OPKG_PKG_DIR}/CONTROL/postrm <<'EOF'
rm -f /etc/rcU.d/S53kudzu-bs-analytics /etc/rcK.d/K53kudzu-bs-analytics
EOF
chmod 755 ${OPKG_PKG_DIR}/CONTROL/postrm

rm -f *.ipk
opkg-build -o root -g root ${OPKG_PKG_DIR}
mv *.ipk ${ASSETS_DIR}
