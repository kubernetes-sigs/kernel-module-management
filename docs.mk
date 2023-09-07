.PHONY: docs

docs: bin/plantuml.jar
	make -C docs

bin/plantuml.jar:
	mkdir -p bin
	curl -Lo $@ https://github.com/plantuml/plantuml/releases/download/v1.2023.10/plantuml-1.2023.10.jar
