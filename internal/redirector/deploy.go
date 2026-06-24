package redirector

import "fmt"

// Standard on-box paths for the redirector's nginx config.
const (
	// StagePath is where the rendered config is uploaded before install.
	StagePath = "/tmp/rinfra-redirector.conf"
	// InstallPath is where nginx loads it from (conf.d is included by the
	// default nginx.conf on Debian/Ubuntu and Amazon Linux/RHEL alike).
	InstallPath = "/etc/nginx/conf.d/rinfra-redirector.conf"
)

// InstallScript returns a bash script that installs nginx (detecting the host's
// package manager so it works on the Debian/Ubuntu and Amazon Linux/RHEL images
// RInfra provisions), moves the staged config into place, validates it, and
// (re)starts nginx. It is idempotent — safe to re-run on every apply.
//
// The config is expected to already be uploaded to stagePath; the script does
// not embed it, so a large/odd config never has to be shell-escaped.
func InstallScript(stagePath, installPath string) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

# Install nginx via whichever package manager the image provides.
if ! command -v nginx >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -y && apt-get install -y nginx
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y nginx
  elif command -v yum >/dev/null 2>&1; then
    yum install -y nginx
  else
    echo "no supported package manager (apt/dnf/yum) found" >&2
    exit 1
  fi
fi

install -d -m 0755 /etc/nginx/conf.d /etc/rinfra/redirector
install -m 0644 %q %q

# Validate before (re)starting so a bad config never takes nginx down.
nginx -t
systemctl enable nginx >/dev/null 2>&1 || true
systemctl restart nginx || (service nginx restart)
echo "rinfra-redirector applied"
`, stagePath, installPath)
}
