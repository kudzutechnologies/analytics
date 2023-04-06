#!/bin/sh
########################
OPKG_PKG_NAME="kudzu-forwarder"
VERSION="0.1.8"
ARCH="klkgw"
########################
ASSETS_DIR="/assets/pkg"
BUILD_DIR=$(mktemp -d -t opkg_build_XXXXXXX)
OPKG_PKG_DIR="${BUILD_DIR}/package"

# -- Create meta-file --

rm -rf ${OPKG_PKG_DIR}
mkdir -p ${OPKG_PKG_DIR}/CONTROL
cp ${ASSETS_DIR}/ipk/control ${OPKG_PKG_DIR}/CONTROL/control

# -- Create package files --

# Copy the etc files
cp -vr ${ASSETS_DIR}/etc ${OPKG_PKG_DIR}/etc

# Copy the binary files
mkdir -p ${OPKG_PKG_DIR}/usr/bin
cp -v /assets/bin/kudzu-forwarder-arm7 ${OPKG_PKG_DIR}/usr/bin/kudzu-forwarder

# -- Create control files --

# Pre-install
cat > ${OPKG_PKG_DIR}/CONTROL/preinst <<'EOF'
if [[ -f /etc/kudzu-forwarder.conf ]]; then
  cp /etc/kudzu-forwarder.conf /etc/kudzu-forwarder.conf.prev
fi
if [[ -f /etc/default/kudzu-forwarder ]]; then
  cp /etc/default/kudzu-forwarder /etc/default/kudzu-forwarder.prev
fi
EOF
chmod 755 ${OPKG_PKG_DIR}/CONTROL/preinst

# Post-install script
cat > ${OPKG_PKG_DIR}/CONTROL/postinst <<'EOF'
ln -s ../init.d/kudzu-forwarder /etc/rcU.d/S54kudzu-forwarder
ln -s ../init.d/kudzu-forwarder /etc/rcK.d/K54kudzu-forwarder
if [[ -f /etc/kudzu-forwarder.conf.prev ]]; then
  mv /etc/kudzu-forwarder.conf.prev /etc/kudzu-forwarder.conf
fi
if [[ -f /etc/default/kudzu-forwarder.prev ]]; then
  mv /etc/default/kudzu-forwarder.prev /etc/default/kudzu-forwarder
fi
EOF
chmod 755 ${OPKG_PKG_DIR}/CONTROL/postinst

# Post-remove script
cat > ${OPKG_PKG_DIR}/CONTROL/postrm <<'EOF'
rm /etc/rcU.d/S54kudzu-forwarder
rm /etc/rcK.d/K54kudzu-forwarder
EOF
chmod 755 ${OPKG_PKG_DIR}/CONTROL/postrm

# Generate the package
rm -f *.ipk
opkg-build -o root -g root ${OPKG_PKG_DIR} >log
cat log
rm -f log

# --- Move out of the container --
mv *.ipk ${ASSETS_DIR}