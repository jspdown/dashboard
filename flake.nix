{
  description = "Personal dashboard";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    systems.url = "github:nix-systems/default";
    gomod2nix.url = "github:nix-community/gomod2nix";
    gomod2nix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    inputs@{ flake-parts, ... }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = import inputs.systems;
      imports = [
        ./nix/go-env.nix
        ./nix/packages.nix
        ./nix/image.nix
        ./nix/lint.nix
        ./nix/tests.nix
        ./nix/apps
        ./nix/devshell.nix
      ];

      perSystem =
        { system, ... }:
        {
          _module.args.pkgs = import inputs.nixpkgs {
            inherit system;
            overlays = [ inputs.gomod2nix.overlays.default ];
          };
        };
    };
}
