# Vendor tree and pre-compiled dependency GOCACHE for a Go module.
# Shared by packages (build), lint, and tests; identical inputs mean
# all consumers reuse one store path.
{
  perSystem =
    { pkgs, ... }:
    {
      _module.args.goEnv =
        pwd: modules:
        pkgs.buildGoApplication {
          pname = "vendor-env";
          version = "0-unstable";
          src = pwd;
          inherit pwd modules;
          inherit (pkgs) go;
          CGO_ENABLED = 0;
        };
    };
}
