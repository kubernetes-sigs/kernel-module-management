all: diagrams build

.PHONY: diagrams

diagrams:
	cd mkdocs/developer && $(MAKE)

build:
	python -m mkdocs build

clean:
	cd mkdocs/developer && $(MAKE) clean
	rm -fr site
