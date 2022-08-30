.PHONY: docs

docs: bin/plantuml.jar
	make -C docs

bin/plantuml.jar:
	mkdir -p bin
	curl -Lo $@ https://sourceforge.net/projects/plantuml/files/1.2022.2/plantuml.1.2022.2.jar/download