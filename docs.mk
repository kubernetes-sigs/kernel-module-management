.PHONY: docs

docs: bin/plantuml.jar
	make -C docs

bin/plantuml.jar:
	mkdir -p bin
	curl -Lo $@ https://github.com/plantuml/plantuml/releases/download/v1.2022.7/plantuml-1.2022.7.jar
