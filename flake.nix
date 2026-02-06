{
  description = "Stellar CLI - Go development environment";

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
            go

            # Go development tools
            gopls
            gotools
            go-tools
          ];

          shellHook = ''
            export GOPATH="$HOME/go"
            export PATH="$GOPATH/bin:$PATH"
            export GOPROXY="https://proxy.golang.org,direct"
            export GOSUMDB="sum.golang.org"

            echo "✳ Stellar CLI development environment loaded!"
          '';
        };

        # Optional: Define the package itself for `nix build`
        packages.default = pkgs.buildGoModule {
          pname = "stellar";
          version = "0.1.0";
          
          src = ./.;
          
          # This will need to be updated after first `go mod download`
          # Run: nix-shell -p nix-prefetch-git --run "nix hash to-sri --type sha256 $(nix-prefetch-git --url . --rev HEAD | jq -r .sha256)"
          vendorHash = null; # or "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
          
          meta = with pkgs.lib; {
            description = "Starship theme manager";
            homepage = "https://github.com/a3chron/stellar";
            license = licenses.mit;
            maintainers = [ ];
          };
        };
      }
    );
}