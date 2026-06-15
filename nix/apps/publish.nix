# `nix run .#publish -- <git-ref>` — push the OCI image to GHCR under the tags the ref maps to.
{ inputs, ... }:
{
  perSystem =
    {
      pkgs,
      self',
      lib,
      ...
    }:
    let
      rev = inputs.self.rev or inputs.self.dirtyRev or "unknown";
      shortRev = builtins.substring 0 7 rev;

      repo = "ghcr.io/jspdown/dashboard";
    in
    {
      apps.publish = {
        type = "app";
        meta.description = "Push the OCI image to GHCR under the tags a git ref maps to";
        program = lib.getExe (
          pkgs.writeShellApplication {
            name = "publish";
            runtimeInputs = [
              pkgs.bash
              pkgs.skopeo
            ];
            text = ''
              ref="''${1:?usage: publish <git-ref>}"

              mapfile -t tags < <(bash ${../../scripts/image-tags.sh} "$ref" "${shortRev}")

              for tag in "''${tags[@]}"; do
                echo "Pushing ${repo}:$tag"
                skopeo copy --insecure-policy \
                  "docker-archive:${self'.packages.image}" \
                  "docker://${repo}:$tag"
              done
            '';
          }
        );
      };
    };
}
