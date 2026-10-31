# A bounded cache definition for a NixOS module.
{ lib, pkgs, config, ... }:

let
  inherit (lib) mkOption types;
  capacity = 16;

  defaults = {
    enable = false;
    size = capacity;
  };
in
{
  options.services.cache = {
    enable = mkOption {
      type = types.bool;
      default = defaults.enable;
      description = "Whether the cache is enabled.";
    };
    size = mkOption {
      type = types.int;
      default = defaults.size;
    };
  };

  config = lib.mkIf config.services.cache.enable {
    systemd.services.cache = {
      wantedBy = [ "multi-user.target" ];
      script = ''
        echo "starting cache with ${toString config.services.cache.size} slots"
        exec ${pkgs.busybox}/bin/sh -c "true"
      '';
    };
  };
}
