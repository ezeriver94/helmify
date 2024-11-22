package service

import (
	"fmt"
	"io"
	"text/template"

	"github.com/ezeriver94/helmify/pkg/helmify"
	"github.com/ezeriver94/helmify/pkg/processor"
	yamlformat "github.com/ezeriver94/helmify/pkg/yaml"
	"github.com/iancoleman/strcase"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
)

var ingressTempl, _ = template.New("ingress").Parse(
	`{{ .Meta }}
{{ .Spec }}`)

var ingressGVC = schema.GroupVersionKind{
	Group:   "networking.k8s.io",
	Version: "v1",
	Kind:    "Ingress",
}

// NewIngress creates processor for k8s Ingress resource.
func NewIngress() helmify.Processor {
	return &ingress{}
}

type ingress struct{}

// Process k8s Service object into template. Returns false if not capable of processing given resource type.
func (r ingress) Process(appMeta helmify.AppMetadata, obj *unstructured.Unstructured) (bool, helmify.Template, error) {
	if obj.GroupVersionKind() != ingressGVC {
		return false, nil, nil
	}
	ing := networkingv1.Ingress{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &ing)
	if err != nil {
		return true, nil, fmt.Errorf("%w: unable to cast to ingress", err)
	}
	values := helmify.Values{}

	meta, err := processor.ProcessObjMeta(appMeta, obj)
	if err != nil {
		return true, nil, err
	}
	name := appMeta.TrimName(obj.GetName())
	err = processIngressSpec(name, appMeta, &ing.Spec, &values)
	if err != nil {
		return true, nil, err
	}

	spec, err := yamlformat.Marshal(map[string]interface{}{"spec": &ing.Spec}, 0)
	if err != nil {
		return true, nil, err
	}

	return true, &ingressResult{
		name:   name + ".yaml",
		values: values,
		data: struct {
			Meta string
			Spec string
		}{Meta: meta, Spec: spec},
	}, nil
}

func processIngressSpec(name string, appMeta helmify.AppMetadata, ing *networkingv1.IngressSpec, values *helmify.Values) error {
	nameCamel := strcase.ToLowerCamel(name)
	if ing.DefaultBackend != nil && ing.DefaultBackend.Service != nil {
		ing.DefaultBackend.Service.Name = appMeta.TemplatedName(ing.DefaultBackend.Service.Name)
	}
	if ing.IngressClassName != nil {
		ing.IngressClassName = ptr.To(fmt.Sprintf("{{ .Values.%s.class }}", nameCamel))
		values.Add("", name, "class")
	}

	for i := range ing.Rules {
		rule := ing.Rules[i]
		if rule.Host != "" {
			rule.Host = "{{ .Values.ingress.host }}"
			values.Add("", name, "host")
		}
		if rule.IngressRuleValue.HTTP != nil {
			for j := range rule.IngressRuleValue.HTTP.Paths {
				if rule.IngressRuleValue.HTTP.Paths[j].Backend.Service != nil {
					rule.IngressRuleValue.HTTP.Paths[j].Backend.Service.Name = appMeta.TemplatedName(rule.IngressRuleValue.HTTP.Paths[j].Backend.Service.Name)
				}
			}
		}
	}
	for i := range ing.TLS {
		if len(ing.TLS[i].Hosts) == 0 {
			continue
		}
		if len(ing.TLS[i].Hosts) > 1 {
			return fmt.Errorf("multiple hosts in TLS not supported")
		}
		ing.TLS[i].Hosts[0] = "{{ .Values.ingress.host }}"
	}
	return nil

}

type ingressResult struct {
	values helmify.Values
	name   string
	data   struct {
		Meta string
		Spec string
	}
}

func (r *ingressResult) Filename() string {
	return r.name
}

func (r *ingressResult) Values() helmify.Values {
	return r.values
}

func (r *ingressResult) Write(writer io.Writer) error {
	return ingressTempl.Execute(writer, r.data)
}
