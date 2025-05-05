{ pkgs ? import <nixpkgs> {} }:
pkgs.mkShell {
  packages = [ pkgs.go ];
  inputsFrom = [
    pkgs.glibc
    pkgs.glfw
  ];

  # inputsFrom = [ pkgs.hello pkgs.gnutar ];

  shellHook = ''

  '';
}
