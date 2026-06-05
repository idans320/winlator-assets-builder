# NDK path matches mesa/build.sh pattern.
# Set NDK before invoking, e.g.:  NDK=$HOME/Android/Sdk/ndk/28.2.13676358 make
NDK ?= $(HOME)/Android/Sdk/ndk/28.2.13676358
SDK_VER := 35
TARGET := aarch64-linux-android$(SDK_VER)
NDK_CLANG := $(NDK)/toolchains/llvm/prebuilt/linux-x86_64/bin
CC := $(NDK_CLANG)/$(TARGET)-clang

CFLAGS := -std=c11 -Wall -Wextra -Werror -O2 -g -I .
LDFLAGS := -lvulkan -landroid

.PHONY: all clean

all: probe_draw

probe_draw: probe_draw.c vulkan_common.h
ifeq ($(wildcard $(CC)),)
	$(error NDK compiler not found at $(CC). Set NDK to your NDK root)
endif
	$(CC) $(CFLAGS) -o $@ $< $(LDFLAGS)
	@file $@

clean:
	rm -f probe_draw
