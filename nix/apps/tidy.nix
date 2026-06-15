# `nix run .#tidy` — tidy go.mod and regenerate the gomod2nix lockfiles.
{
  perSystem =
    {
      pkgs,
      inputs',
      lib,
      ...
    }:
    {
      apps.tidy = {
        type = "app";
        meta.description = "Tidy go.mod and regenerate the gomod2nix lockfiles";
        program = lib.getExe (
          pkgs.writeShellApplication {
            name = "tidy";
            runtimeInputs = [
              pkgs.go
              inputs'.gomod2nix.packages.default
            ];
            text = ''
              for module in api e2e; do
                (
                  cd "$module"
                  go mod tidy
                  # --with-deps records the dependency closure as
                  # cachePackages, which the flake turns into a pre-compiled
                  # GOCACHE for the lint checks and the api build.
                  gomod2nix generate --with-deps
                )
              done
            '';
          }
        );
      };
    };
}
