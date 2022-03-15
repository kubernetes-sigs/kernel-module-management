all: diagrams build

.PHONY: diagrams

diagrams:
	cd mkdocs/developer && $(MAKE)

build:
	mkdocs build

clean:
	cd mkdocs/developer && $(MAKE) clean
	rm -fr site
