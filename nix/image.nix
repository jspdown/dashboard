# The OCI image (`nix build .#image`) and its checks.
{ inputs, ... }:
{
  perSystem =
    {
      pkgs,
      self',
      ...
    }:
    let
      rev = inputs.self.rev or inputs.self.dirtyRev or "unknown";
      shortRev = builtins.substring 0 7 rev;

      # Reshape a "YYYYMMDDHHMMSS" stamp into an RFC 3339 timestamp. Used on
      # self.lastModifiedDate so the image's created date is meaningful yet still
      # a pure function of the commit (no impure "now").
      toRfc3339 =
        d:
        let
          s = at: len: builtins.substring at len d;
        in
        "${s 0 4}-${s 4 2}-${s 6 2}T${s 8 2}:${s 10 2}:${s 12 2}Z";
      created = toRfc3339 (inputs.self.lastModifiedDate or "19700101000001");

      appRoot = pkgs.runCommand "dashboard-app" { } ''
        mkdir -p $out/app
        cp -r ${self'.packages.app}/. $out/app/
      '';
    in
    {
      packages.image = pkgs.dockerTools.buildLayeredImage {
        name = "dashboard";
        tag = shortRev;
        inherit created;

        contents = [
          self'.packages.api # /bin/dashboard
          appRoot # /app
          pkgs.cacert # /etc/ssl/certs/ca-bundle.crt
          pkgs.tzdata # /share/zoneinfo
          pkgs.fakeNss # /etc/passwd (root + nobody), /etc/group, /tmp
        ];

        config = {
          Entrypoint = [ "/bin/dashboard" ];
          Cmd = [ "serve" ];
          User = "nobody:nobody";
          ExposedPorts = {
            "8080/tcp" = { };
          };
          Env = [
            "DASHBOARD_WEB_DIR=/app"
            "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt"
            "ZONEINFO=${pkgs.tzdata}/share/zoneinfo"
          ];
          Labels = {
            "org.opencontainers.image.title" = "dashboard";
            "org.opencontainers.image.description" = "Pull request dashboard";
            "org.opencontainers.image.source" = "https://github.com/jspdown/dashboard";
            "org.opencontainers.image.revision" = rev;
            "org.opencontainers.image.created" = created;
            "org.opencontainers.image.licenses" = "MIT";
          };
        };
      };

      checks = {
        # Build the image on every PR (no push) so a broken derivation surfaces
        # before a release tag, not at release time.
        image = self'.packages.image;

        # The release tag-mapping is the only branchy logic in the publish path,
        # so it gets its own assertion. Mirrors scripts/image-tags.sh.
        tag-logic = pkgs.runCommand "tag-logic-test" { } ''
          script=${../scripts/image-tags.sh}
          fail() {
            echo "tag-logic: $1" >&2
            echo "  expected: $2" >&2
            echo "  got:      $3" >&2
            exit 1
          }
          expect() {
            got=$(bash "$script" "$2" abc1234 | tr '\n' ' ')
            got=''${got% }
            [ "$got" = "$3" ] || fail "$1" "$3" "$got"
          }
          expect "release"      refs/tags/v1.2.3      "1.2.3 1.2 sha-abc1234 latest"
          expect "pre-release"  refs/tags/v1.2.3-rc1  "1.2.3-rc1 sha-abc1234"
          expect "non-tag ref"  refs/heads/main       "sha-abc1234"
          touch $out
        '';
      };
    };
}
