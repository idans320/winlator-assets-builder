# Runtime image for testing ARM64 Wine headlessly on an x86-64 host.
#
# The container is arm64, so it runs under qemu-user via binfmt. Everything the
# tests need is installed here once; rebuild only when this file changes.
FROM --platform=linux/arm64 ubuntu:26.10

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
      build-essential autoconf bison flex pkg-config python3 curl xz-utils file \
      libx11-dev libxext-dev libxrender-dev libxrandr-dev libxi-dev libxcursor-dev \
      libxfixes-dev libxcomposite-dev libxinerama-dev libxxf86vm-dev \
      libxkbcommon-dev libxkbcommon-x11-dev libfreetype-dev libfontconfig-dev \
      libgl-dev libegl-dev libgnutls28-dev libdrm-dev libpulse-dev libasound2-dev \
      libudev-dev libvulkan-dev \
      xvfb libx11-6 libxext6 libxrender1 libxrandr2 libxi6 libxcursor1 libxfixes3 \
      libxcomposite1 libxinerama1 libxxf86vm1 libxkbcommon0 libxkbcommon-x11-0 \
      libfreetype6 libfontconfig1 libgnutls30t64 libdrm2 libvulkan1 libgl1 libegl1 \
      libpulse0 libasound2t64 mesa-vulkan-drivers vulkan-tools \
    && rm -rf /var/lib/apt/lists/*

# The build tree, the Wine source (read-only) and the prefix live on the host and
# are mounted at these paths; see run.sh and build-wine.sh.
WORKDIR /wine
CMD ["bash"]
