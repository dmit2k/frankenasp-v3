CC ?= cc
CFLAGS ?= -std=c11 -O2 -Wall -Wextra -Werror
.PHONY: test sanitize clean
build:
	mkdir -p build
build/test_front: src/front.c src/front.h src/resampler_coeffs.h tests/test_front.c | build
	$(CC) $(CFLAGS) -o $@ src/front.c tests/test_front.c -lm
test: build/test_front
	./build/test_front
sanitize: | build
	$(CC) $(CFLAGS) -g -O1 -fsanitize=address,undefined -fno-omit-frame-pointer -o build/test_front_san src/front.c tests/test_front.c -lm
	./build/test_front_san
clean:
	rm -f build/test_front build/test_front_san
