# Buildable artifacts (`nix build .#<name>`).
{ inputs, ... }:
{
  perSystem =
    {
      pkgs,
      self',
      goEnv,
      ...
    }:
    let
      # Build info to inject into the dashboard.
      buildinfoPkg = "github.com/jspdown/dashboard/api/pkg/buildinfo";
      revision = inputs.self.rev or inputs.self.dirtyRev or "unknown";
      revModified = if inputs.self ? rev then "false" else "true";
      buildTime = inputs.self.lastModifiedDate or "unknown";

      vendorEnv = goEnv ../api ../api/gomod2nix.toml;
      inherit (vendorEnv) goCacheEnv;
    in
    {
      packages = {
        # Build the dashboard api component.
        api =
          (pkgs.buildGoApplication {
            pname = "dashboard-api";
            version = "0-unstable";
            src = ../api;
            pwd = ../api;
            modules = ../api/gomod2nix.toml;
            subPackages = [ "cmd/dashboard" ];
            inherit (pkgs) go;
            CGO_ENABLED = 0;
            disableGoCache = true;
            ldflags = [
              "-s"
              "-w"
              "-X"
              "${buildinfoPkg}.revision=${revision}"
              "-X"
              "${buildinfoPkg}.modified=${revModified}"
              "-X"
              "${buildinfoPkg}.buildTime=${buildTime}"
            ];
          }).overrideAttrs
            { goCacheDir = goCacheEnv; };

        # Build the dashboard app component.
        app = pkgs.buildNpmPackage {
          pname = "dashboard-app";
          version = "0-unstable";
          src = ../app;
          npmDeps = pkgs.importNpmLock { npmRoot = ../app; };
          npmConfigHook = pkgs.importNpmLock.npmConfigHook;
          npmFlags = [ "--legacy-peer-deps" ];
          installPhase = ''
            runHook preInstall
            cp -r dist $out
            runHook postInstall
          '';
        };

        # Build the dashboard from the api and the app components.
        default = pkgs.runCommand "dashboard" { } ''
          mkdir -p $out/bin $out/dist
          cp ${self'.packages.api}/bin/dashboard $out/bin/api
          cp -r ${self'.packages.app}/. $out/dist/
        '';
      };
    };
}
