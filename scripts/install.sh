#!/bin/bash
set -e

if [ "$EUID" -ne 0 ]; then
  echo "Please run as root"
  exit 1
fi

echo "Stopping maps6d service if running..."
systemctl stop maps6d || true

echo "Creating directories..."
mkdir -p /home/pi/maps6/data

echo "Copying binaries..."
cp maps6d maps6ctl /home/pi/maps6/
chmod +x /home/pi/maps6/maps6d
chmod +x /home/pi/maps6/maps6ctl

echo "Setting up symlinks..."
ln -sf /home/pi/maps6/maps6ctl /usr/local/bin/maps6ctl

echo "Copying config..."
if [ ! -f /home/pi/maps6/maps6.yaml ]; then
  cp maps6.yaml /home/pi/maps6/
else
  cp maps6.yaml /home/pi/maps6/maps6.yaml.new
  echo "Existing config kept. New config saved as maps6.yaml.new"
fi

echo "Installing systemd service..."
cp maps6d.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable maps6d

echo "Starting service..."
systemctl start maps6d

echo "Installation complete!"
echo "Use 'maps6ctl status' to check daemon status."
