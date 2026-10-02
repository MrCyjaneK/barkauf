Name:       barkauf

%global _missing_build_ids_terminate_build 0

%{!?qtc_qmake5:%define qtc_qmake5 %qmake5}
%{!?qtc_make:%define qtc_make make}

Summary:    Barkauf mainnet wallet demo
Version:    0.1
Release:    36
Group:      Applications/System
License:    BSD and MIT and Apache-2.0 and LGPLv2
Source0:    %{name}-%{version}.tar.bz2
Requires:   mapplauncherd-booster-silica-qt5
Requires:   sailfishsilica-qt5
Requires:   qt5-qtmultimedia
BuildRequires:  pkgconfig(sailfishapp)
BuildRequires:  pkgconfig(Qt5Quick)
BuildRequires:  pkgconfig(Qt5Qml)
BuildRequires:  pkgconfig(Qt5Core)
BuildRequires:  pkgconfig(Qt5DBus)
BuildRequires:  pkgconfig(Qt5Multimedia)
BuildRequires:  pkgconfig(qdeclarative5-boostable)
BuildRequires:  gcc

%description
Minimal Sailfish demo: a mainnet Bark wallet, a QR scanner, and NFC text sharing of a BIP 321 payment URI.

%prep
%setup -q -n %{name}-%{version}

%build
export CGO_ENABLED=1 GOTOOLCHAIN=local
export GOCACHE=/tmp/sfosbuild-gocache GOPATH=/tmp/sfosbuild-gopath
export GOFLAGS=-mod=vendor GOPROXY=off GOSUMDB=off

( cd native/wallet && go build -o barkauf-wallet . )

%qtc_qmake5
%qtc_make %{?_smp_mflags}

%install
rm -rf %{buildroot}
%qtc_make install INSTALL_ROOT=%{buildroot}
install -D -m 755 native/wallet/barkauf-wallet %{buildroot}%{_libexecdir}/%{name}/barkauf-wallet

%files
%defattr(-,root,root,-)
%{_datadir}/applications/%{name}.desktop
%{_datadir}/icons/hicolor/86x86/apps/%{name}.png
%{_datadir}/icons/hicolor/108x108/apps/%{name}.png
%{_datadir}/icons/hicolor/128x128/apps/%{name}.png
%{_datadir}/icons/hicolor/172x172/apps/%{name}.png
%{_datadir}/icons/hicolor/256x256/apps/%{name}.png
%{_datadir}/%{name}/qml
%{_bindir}/%{name}
%dir %{_libexecdir}/%{name}
%{_libexecdir}/%{name}/barkauf-wallet
