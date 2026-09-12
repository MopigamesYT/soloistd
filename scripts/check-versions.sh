#!/bin/sh
# SPDX-License-Identifier: 0BSD
#
# The release version is repeated in the program and in every packaging
# recipe. Fail when they drift apart.

set -eu

cd "$(dirname "$0")/.."

source_version=$(sed -n 's/^const version = "\(.*\)"$/\1/p' main.go)
deb_version=$(sed -n '1s/^soloistd (\(.*\)-[^-]*).*$/\1/p' debian/changelog)
rpm_version=$(sed -n 's/^Version: *\([^ ]*\) *$/\1/p' fedora/soloistd.spec)
aur_version=$(sed -n 's/^pkgver=\(.*\)$/\1/p' aur/PKGBUILD)
srcinfo_version=$(sed -n 's/^	pkgver = \(.*\)$/\1/p' aur/.SRCINFO)
man_version=$(sed -n 's/^\.TH .* "soloistd \(.*\)" .*$/\1/p' doc/soloistd.1)

status=0
report() {
	printf '%-24s %s\n' "$1" "$2"
	if [ "$2" != "$source_version" ]; then
		status=1
	fi
}

if [ -z "$source_version" ]; then
	echo "check-versions: no version found in main.go" >&2
	exit 1
fi

printf '%-24s %s\n' 'main.go' "$source_version"
report 'debian/changelog' "$deb_version"
report 'fedora/soloistd.spec' "$rpm_version"
report 'aur/PKGBUILD' "$aur_version"
report 'aur/.SRCINFO' "$srcinfo_version"
report 'doc/soloistd.1' "$man_version"

if [ "$status" -ne 0 ]; then
	echo >&2
	echo "check-versions: packaging versions disagree with main.go ($source_version)" >&2
fi

exit "$status"
