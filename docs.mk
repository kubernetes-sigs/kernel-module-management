.PHONY: docs

docs: bin/plantuml.jar
	make -C docs

bin/plantuml.jar:
	mkdir -p bin
	# Seems that SF is showing updated one, but no download of the previous one we were using
	# curl -Lo $@ https://sourceforge.net/projects/plantuml/files/1.2022.2/plantuml.1.2022.2.jar/download
	curl -Lo $@ https://sourceforge.net/projects/plantuml/files/1.2021.9/plantuml.1.2021.9.jar/download
