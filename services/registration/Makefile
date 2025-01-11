# Variables to match files in the current directory
INPUTS = $(wildcard ./*.vis)
TXTS = $(wildcard ./*.txt.orig)
VISE_PATH := ../../go-vise

# Rule to build .bin files from .vis files
%.vis:
	go run  $(VISE_PATH)/dev/asm/main.go -f pp.csv $(basename $@).vis > $(basename $@).bin
	@echo "Built $(basename $@).bin from $(basename $@).vis"

# Rule to copy .orig files to .txt
%.txt.orig:
	cp -v $(basename $@).orig $(basename $@)
	@echo "Copied $(basename $@).orig to $(basename $@)"

# 'all' target depends on all .vis and .txt.orig files
all: $(INPUTS) $(TXTS)
	@echo "Running all: $(INPUTS) $(TXTS)"

clean: 
	rm -vf *.bin

.PHONY: clean
