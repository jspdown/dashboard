# `nix run .#screenshots` — regenerate the canonical UI screenshots.
#
# Same impure runner shape as test-e2e (host Docker daemon + Chrome), but it
# runs the e2e/screenshots suite, which writes PNGs as a side effect. It serves
# the Nix-built web bundle (DASHBOARD_WEB_DIR), so the captures cannot drift
# from what ships in the image. Output goes to docs/screenshots; OUT=<dir>
# overrides for ad-hoc captures.
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
      apps.screenshots = {
        type = "app";
        meta.description = "Regenerate canonical UI screenshots (OUT=<dir> overrides)";
        program = lib.getExe (
          pkgs.writeShellApplication {
            name = "screenshots";
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
              export DASHBOARD_SCREENSHOT_DIR="''${OUT:-$root/docs/screenshots}"
              mkdir -p "$DASHBOARD_SCREENSHOT_DIR"
              cd "$root/e2e"
              exec go test -count=1 -timeout 10m ./screenshots/...
            '';
          }
        );
      };
    };
}
