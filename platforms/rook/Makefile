CC ?= cc
CFLAGS ?= -std=c11 -O2 -Wall -Wextra -Wpedantic -Wconversion -Wshadow -Werror
RUNTIME = src/descriptor.c src/io.c src/runtime.c src/factory_adapter.c src/child.c
MOCK = tests/mock_vendor.c
.PHONY: all test sanitize clean
all: build/codec_probe build/runtime_mock
build:
	mkdir -p build
build/codec_probe: src/descriptor.c src/descriptor.h tests/codec_probe.c | build
	$(CC) $(CFLAGS) -o $@ src/descriptor.c tests/codec_probe.c
build/runtime_mock: $(RUNTIME) $(MOCK) src/runtime.h src/io.h src/factory_adapter.h src/descriptor.h | build
	$(CC) $(CFLAGS) -DASP_TEST_ONLY -o $@ $(RUNTIME) $(MOCK)
build/test_alias_policy: tests/test_alias_policy.c launcher/alias_policy.c launcher/alias_policy.h | build
	$(CC) $(CFLAGS) -o $@ tests/test_alias_policy.c launcher/alias_policy.c
build/io_faults.so: tests/io_faults.c | build
	$(CC) -std=c11 -Wall -Wextra -Wpedantic -Werror -fPIC -shared -o $@ tests/io_faults.c -ldl
build/test_sha256: tests/test_sha256.c src/sha256.c src/sha256.h | build
	$(CC) $(CFLAGS) -D_GNU_SOURCE -o $@ tests/test_sha256.c src/sha256.c
test: all build/test_alias_policy build/io_faults.so build/test_sha256
	./build/test_alias_policy
	./build/test_sha256
	python3 tests/test_codec.py build/codec_probe
	python3 tests/test_runtime.py build/runtime_mock
	python3 tests/test_launcher.py
asan: | build
	$(CC) -std=c11 -g -O1 -Wall -Wextra -Wpedantic -Werror -fsanitize=address -fno-omit-frame-pointer -o build/codec_probe_asan src/descriptor.c tests/codec_probe.c
	python3 tests/test_codec.py build/codec_probe_asan
	$(CC) -std=c11 -g -O1 -Wall -Wextra -Wpedantic -Werror -fsanitize=address -fno-omit-frame-pointer -DASP_TEST_ONLY -o build/runtime_mock_asan $(RUNTIME) $(MOCK)
	python3 tests/test_runtime.py build/runtime_mock_asan
ubsan: | build
	$(CC) -std=c11 -g -O1 -Wall -Wextra -Wpedantic -Werror -fsanitize=undefined -fno-omit-frame-pointer -o build/codec_probe_ubsan src/descriptor.c tests/codec_probe.c
	python3 tests/test_codec.py build/codec_probe_ubsan
	$(CC) -std=c11 -g -O1 -Wall -Wextra -Wpedantic -Werror -fsanitize=undefined -fno-omit-frame-pointer -DASP_TEST_ONLY -o build/runtime_mock_ubsan $(RUNTIME) $(MOCK)
	python3 tests/test_runtime.py build/runtime_mock_ubsan
sanitize: asan ubsan
clean:
	rm -f build/codec_probe build/codec_probe_asan build/codec_probe_ubsan build/runtime_mock build/runtime_mock_asan build/runtime_mock_ubsan build/test_alias_policy build/test_sha256 build/io_faults.so
