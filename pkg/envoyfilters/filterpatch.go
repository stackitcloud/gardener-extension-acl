package envoyfilters

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// filterPatch represents the object beneath EnvoyFilter.spec.configPatches.patch.value
// It holds the name of the filter and its typed config to inject into the envoy config
type filterPatch struct {
	Name   string        `json:"name"`
	Config proto.Message `json:"typed_config"`
}

// AsStructPB returns FilterPatch represented as a structpb.Struct
func (f *filterPatch) AsStructPB() (*structpb.Struct, error) {
	typedConfig, err := f.typedConfig()
	if err != nil {
		return nil, err
	}
	return &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"name":         structpb.NewStringValue(f.Name),
			"typed_config": structpb.NewStructValue(typedConfig),
		},
	}, nil
}

func (f *filterPatch) typedConfig() (*structpb.Struct, error) {
	raw, err := protojson.MarshalOptions{
		UseProtoNames: true,
	}.Marshal(f.Config)
	if err != nil {
		return nil, err
	}
	s := new(structpb.Struct)
	if err := protojson.Unmarshal(raw, s); err != nil {
		return nil, err
	}
	typeName := fmt.Sprintf("type.googleapis.com/%s", proto.MessageName(f.Config))
	s.Fields["@type"] = structpb.NewStringValue(typeName)
	return s, nil
}
