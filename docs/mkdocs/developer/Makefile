all: diagrams/reconciliation-module.png

diagrams/%.png: diagrams/%.puml
	java -jar ../../../bin/plantuml.jar -printfonts
	java -Djava.awt.headless=true -jar ../../../bin/plantuml.jar -v -nometadata -p < $< > $@

clean:
	rm -f *.png
