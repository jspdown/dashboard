# `nix run .#test-e2e` — run the full e2e suite.
#
# Impure by necessity: the harness drives a host Docker daemon (testcontainers
# Postgres) and Chrome (chromedp), so this cannot be a hermetic cached check.
# It serves the Nix-built web bundle (DASHBOARD_WEB_DIR), so the suite
# exercises exactly what ships in the image. CHROME_PATH points chromedp at
# the pinned Chromium on Linux/CI; macOS has no nixpkgs chromium, so the
# harness auto-detects a /Applications Chrome there.
{
  perSystem =
    {
      pkgs,
      self',
      lib,
      ...
    }:
    let
      isLinux = pkgs.stdenv.hostPlatform.isLinux;
    in
    {
      apps.test-e2e = {
        type = "app";
        meta.description = "Run e2e tests";
        program = lib.getExe (
          pkgs.writeShellApplication {
            name = "test-e2e";
            runtimeInputs = [
              pkgs.go
              pkgs.git
            ]
            ++ lib.optional isLinux pkgs.chromium;
            text = ''
              root=$(git rev-parse --show-toplevel)
              # Use the pinned Go's own toolchain and stdlib. A shell that has
              # run `go` before may export GOROOT (and GOTOOLCHAIN auto can
              # pull a newer toolchain), which makes the pinned go binary load
              # a mismatched stdlib.
              export GOTOOLCHAIN=local
              unset GOROOT
              ${lib.optionalString isLinux "export CHROME_PATH=${lib.getExe pkgs.chromium}"}
              export DASHBOARD_WEB_DIR=${self'.packages.app}
              cd "$root/e2e"
              exec go test -count=1 -timeout 10m ./scenarios/...
            '';
          }
        );
      };
    };
}
