{
  description = "Apple Studio Display Brightness Control - Development Environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            # Go development
            go_1_25
            gopls
            golangci-lint
            gosec
            mockgen
            delve

            # Required for sstallion/go-hid (CGO)
            pkg-config
            hidapi
            libusb1

            # For udev development
            systemd

            # Build tools
            go-task

            # D-Bus testing
            dbus
          ];

          shellHook = ''
            export CGO_ENABLED=1
            export PKG_CONFIG_PATH="${pkgs.hidapi}/lib/pkgconfig:${pkgs.libusb1}/lib/pkgconfig:$PKG_CONFIG_PATH"
            echo "Apple Studio Display Brightness development environment loaded"
            echo "Go version: $(go version)"
          '';
        };
      }
    );
}
