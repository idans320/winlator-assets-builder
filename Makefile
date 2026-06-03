.PHONY: mesa fexcore wine box64 dxvk benchmark

mesa:
	devbox run -- ./mesa/build.sh

fexcore:
	devbox run -- ./fexcore/build.sh

wine:
	devbox run -- ./wine/build.sh --setup

box64:
	devbox run -- ./box64/build.sh

dxvk:
	devbox run -- ./dxvk/build.sh

benchmark:
	devbox run -- ./benchmark/build.sh
