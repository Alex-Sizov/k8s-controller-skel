project-dir := $(notdir $(shell pwd))

.PHONY : deepcopy-gen

deepcopy-gen :
	deepcopy-gen --input-dirs ${project-dir}/pkg/apis/mygroup.example.com/v1alpha1 -O zz_generated.deepcopy --output-base ../ --go-header-file ./hack/boilerplate.go.txt
