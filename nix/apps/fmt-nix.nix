# `nix run .#fmt-nix` — format and lint every Nix file in the tree.
{
  perSystem =
    { pkgs, lib, ... }:
    {
      apps.fmt-nix = {
        type = "app";
        meta.description = "Format and lint the Nix flake";
        program = lib.getExe (
          pkgs.writeShellApplication {
            name = "fmt-nix";
            runtimeInputs = [
              pkgs.nixfmt
              pkgs.statix
              pkgs.deadnix
            ];
            text = ''
              shopt -s globstar
              nixfmt ./**/*.nix
              statix check .
              deadnix --fail .
            '';
          }
        );
      };
    };
}
