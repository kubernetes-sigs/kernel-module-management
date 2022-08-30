all: diagrams/reconciliation-module.png

diagrams/%.png: diagrams/%.puml
	java -jar ../../../bin/plantuml.jar -nometadata -p < $< > $@

clean:
	rm -f *.png
