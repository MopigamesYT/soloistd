# SPDX-License-Identifier: 0BSD

# dwz cannot process the DWARF that the Go linker emits, so skip it and leave
# the plain debuginfo extractor to do the work. It needs uncompressed DWARF,
# which the build section asks the Go linker for.
%global _find_debuginfo_dwz_opts %{nil}

Name:           soloistd
Version:        0.2.0
Release:        1%{?dist}
Summary:        Manage Spotify Soloist as a user service with automatic updates

License:        MPL-2.0
URL:            https://github.com/MopigamesYT/soloistd
Source0:        %{url}/archive/refs/tags/v%{version}.tar.gz#/%{name}-%{version}.tar.gz

ExclusiveArch:  x86_64 aarch64 %{arm}

BuildRequires:  golang >= 1.24
BuildRequires:  gcc
Recommends:     systemd

%description
soloistd is a small supervisor for Spotify Soloist. It keeps the Soloist API
key outside the systemd unit, runs Soloist as a systemd user service, and
proactively checks the official CDN for new builds.

When a new official build appears, soloistd downloads and validates it,
atomically replaces its user-owned copy of the Soloist executable, and restarts
Soloist gracefully. The documented "build expired" exit status is retained as an
independent recovery path.

%prep
%autosetup

%build
# Build from the vendored dependencies with the distribution toolchain only;
# the build must never reach the network for a module or a toolchain.
export GOFLAGS="-mod=vendor -trimpath -buildmode=pie"
export GOTOOLCHAIN=local
export GOPATH="%{_builddir}/.gopath"
export GOCACHE="%{_builddir}/.gocache"
export GOMODCACHE="%{_builddir}/.gopath/pkg/mod"
export CGO_ENABLED=1
export CGO_CFLAGS="%{build_cflags}"
export CGO_CXXFLAGS="%{build_cxxflags}"
export CGO_LDFLAGS="%{build_ldflags}"
go build -ldflags="-linkmode=external -compressdwarf=false -extldflags '%{build_ldflags}'" -o %{name} .

%check
export GOFLAGS="-mod=vendor"
export GOTOOLCHAIN=local
export GOPATH="%{_builddir}/.gopath"
export GOCACHE="%{_builddir}/.gocache"
go test ./...

%install
install -D -p -m 0755 %{name} %{buildroot}%{_bindir}/%{name}
install -D -p -m 0644 doc/%{name}.1 %{buildroot}%{_mandir}/man1/%{name}.1

%files
%license LICENSE
%doc README.md
%{_bindir}/%{name}
%{_mandir}/man1/%{name}.1*

%changelog
* Sat Sep 12 2026 Margot <mopigames@proton.me> - 0.2.0-1
- Initial Fedora packaging.
