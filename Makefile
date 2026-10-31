CC ?= cc
CFLAGS ?= -std=c11 -O2 -Wall -Wextra -Wpedantic -Wconversion -Wshadow -Werror
.PHONY: all test sanitize clean
all: build/codec_probe
build:
	mkdir -p build
build/codec_probe: src/descriptor.c src/descriptor.h tests/codec_probe.c | build
	$(CC) $(CFLAGS) -o $@ src/descriptor.c tests/codec_probe.c
test: build/codec_probe
	python3 tests/test_codec.py build/codec_probe
sanitize: | build
	$(CC) -std=c11 -g -O1 -Wall -Wextra -Wpedantic -Werror -fsanitize=address,undefined -fno-omit-frame-pointer -o build/codec_probe_san src/descriptor.c tests/codec_probe.c
	python3 tests/test_codec.py build/codec_probe_san
clean:
	rm -f build/codec_probe build/codec_probe_san
