all: diagrams/reconciliation-daemonset.png diagrams/reconciliation-module.png diagrams/reconciliation-node.png

diagrams/%.png: diagrams/%.puml
	plantuml -p < $< > $@

clean:
	rm -f *.png
