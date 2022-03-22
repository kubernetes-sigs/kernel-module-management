all: diagrams/reconciliation-daemonset.png diagrams/reconciliation-module.png diagrams/reconciliation-node.png

diagrams/%.png: diagrams/%.puml
	java -jar ../../../bin/plantuml.jar -nometadata -p < $< > $@

clean:
	rm -f *.png
