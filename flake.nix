{
  description = "MoonBit - a modern system cleaner for Linux with a TUI and CLI";

  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
  };

  outputs =
    {
      self,
      flake-utils,
      nixpkgs,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        version = "1.4.0";
      in
      {
        packages = rec {
          moonbit = pkgs.buildGoModule {
            pname = "moonbit";
            inherit version;
            src = ./.;

            # Update with the hash nix reports when go.mod/go.sum change.
            vendorHash = "sha256-AIYXk58XHNZMhviqvZf6Ayws50XYm/vYexzk/6oZ10E=";

            subPackages = [ "cmd" ];

            # CGO is not needed; a static binary keeps the closure minimal.
            env.CGO_ENABLED = "0";

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=v${version}"
              "-X main.BuildTime=1970-01-01T00:00:00Z" # reproducible
            ];

            postInstall = ''
              # `subPackages = [ "cmd" ]` produces $out/bin/cmd.
              mv $out/bin/cmd $out/bin/moonbit

              install -Dm644 systemd/moonbit-scan.service $out/lib/systemd/system/moonbit-scan.service
              install -Dm644 systemd/moonbit-scan.timer   $out/lib/systemd/system/moonbit-scan.timer
              install -Dm644 systemd/moonbit-clean.service $out/lib/systemd/system/moonbit-clean.service
              install -Dm644 systemd/moonbit-clean.timer   $out/lib/systemd/system/moonbit-clean.timer
              install -Dm644 systemd/moonbit-daemon.service $out/lib/systemd/system/moonbit-daemon.service

              # The units ship with the from-source /usr/local/bin path; point
              # them at the store path this derivation actually produced.
              substituteInPlace $out/lib/systemd/system/*.service \
                --replace-quiet /usr/local/bin/moonbit $out/bin/moonbit

              install -Dm644 packaging/moonbit.desktop $out/share/applications/moonbit.desktop
              substituteInPlace $out/share/applications/moonbit.desktop \
                --replace-quiet /usr/local/bin/moonbit $out/bin/moonbit
              install -Dm644 packaging/moonbit.svg \
                $out/share/icons/hicolor/scalable/apps/moonbit.svg

              install -Dm644 README.md $out/share/doc/moonbit/README.md
              install -Dm644 LICENSE   $out/share/licenses/moonbit/LICENSE
            '';

            meta = with pkgs.lib; {
              description = "A modern system cleaner for Linux with a TUI and CLI";
              homepage = "https://github.com/Nomadcxx/moonbit";
              license = licenses.gpl3Only;
              mainProgram = "moonbit";
              platforms = platforms.linux;
            };
          };

          default = moonbit;
        };

        apps = rec {
          moonbit = flake-utils.lib.mkApp { drv = self.packages.${system}.moonbit; };
          default = moonbit;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            golangci-lint
            gnumake
            nfpm
          ];
        };

        checks.moonbit = self.packages.${system}.moonbit;
      }
    );
}
