// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.34.1
// 	protoc        v3.21.12
// source: task.proto

package pb

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type RevealStrategy int32

const (
	RevealStrategy_RevealAlways RevealStrategy = 0
	RevealStrategy_RevealNever  RevealStrategy = 1
)

// Enum value maps for RevealStrategy.
var (
	RevealStrategy_name = map[int32]string{
		0: "RevealAlways",
		1: "RevealNever",
	}
	RevealStrategy_value = map[string]int32{
		"RevealAlways": 0,
		"RevealNever":  1,
	}
)

func (x RevealStrategy) Enum() *RevealStrategy {
	p := new(RevealStrategy)
	*p = x
	return p
}

func (x RevealStrategy) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (RevealStrategy) Descriptor() protoreflect.EnumDescriptor {
	return file_task_proto_enumTypes[0].Descriptor()
}

func (RevealStrategy) Type() protoreflect.EnumType {
	return &file_task_proto_enumTypes[0]
}

func (x RevealStrategy) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use RevealStrategy.Descriptor instead.
func (RevealStrategy) EnumDescriptor() ([]byte, []int) {
	return file_task_proto_rawDescGZIP(), []int{0}
}

type HideStrategy int32

const (
	HideStrategy_HideAlways    HideStrategy = 0
	HideStrategy_HideNever     HideStrategy = 1
	HideStrategy_HideOnSuccess HideStrategy = 2
)

// Enum value maps for HideStrategy.
var (
	HideStrategy_name = map[int32]string{
		0: "HideAlways",
		1: "HideNever",
		2: "HideOnSuccess",
	}
	HideStrategy_value = map[string]int32{
		"HideAlways":    0,
		"HideNever":     1,
		"HideOnSuccess": 2,
	}
)

func (x HideStrategy) Enum() *HideStrategy {
	p := new(HideStrategy)
	*p = x
	return p
}

func (x HideStrategy) String() string {
	return protoimpl.X.EnumStringOf(x.Descriptor(), protoreflect.EnumNumber(x))
}

func (HideStrategy) Descriptor() protoreflect.EnumDescriptor {
	return file_task_proto_enumTypes[1].Descriptor()
}

func (HideStrategy) Type() protoreflect.EnumType {
	return &file_task_proto_enumTypes[1]
}

func (x HideStrategy) Number() protoreflect.EnumNumber {
	return protoreflect.EnumNumber(x)
}

// Deprecated: Use HideStrategy.Descriptor instead.
func (HideStrategy) EnumDescriptor() ([]byte, []int) {
	return file_task_proto_rawDescGZIP(), []int{1}
}

type TaskContextForLocation struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	ProjectId     uint64            `protobuf:"varint,1,opt,name=project_id,json=projectId,proto3" json:"project_id,omitempty"`
	Location      *Location         `protobuf:"bytes,2,opt,name=location,proto3" json:"location,omitempty"`
	TaskVariables map[string]string `protobuf:"bytes,3,rep,name=task_variables,json=taskVariables,proto3" json:"task_variables,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

func (x *TaskContextForLocation) Reset() {
	*x = TaskContextForLocation{}
	if protoimpl.UnsafeEnabled {
		mi := &file_task_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *TaskContextForLocation) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TaskContextForLocation) ProtoMessage() {}

func (x *TaskContextForLocation) ProtoReflect() protoreflect.Message {
	mi := &file_task_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TaskContextForLocation.ProtoReflect.Descriptor instead.
func (*TaskContextForLocation) Descriptor() ([]byte, []int) {
	return file_task_proto_rawDescGZIP(), []int{0}
}

func (x *TaskContextForLocation) GetProjectId() uint64 {
	if x != nil {
		return x.ProjectId
	}
	return 0
}

func (x *TaskContextForLocation) GetLocation() *Location {
	if x != nil {
		return x.Location
	}
	return nil
}

func (x *TaskContextForLocation) GetTaskVariables() map[string]string {
	if x != nil {
		return x.TaskVariables
	}
	return nil
}

type TaskContext struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Cwd           *string           `protobuf:"bytes,1,opt,name=cwd,proto3,oneof" json:"cwd,omitempty"`
	TaskVariables map[string]string `protobuf:"bytes,2,rep,name=task_variables,json=taskVariables,proto3" json:"task_variables,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	ProjectEnv    map[string]string `protobuf:"bytes,3,rep,name=project_env,json=projectEnv,proto3" json:"project_env,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

func (x *TaskContext) Reset() {
	*x = TaskContext{}
	if protoimpl.UnsafeEnabled {
		mi := &file_task_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *TaskContext) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TaskContext) ProtoMessage() {}

func (x *TaskContext) ProtoReflect() protoreflect.Message {
	mi := &file_task_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TaskContext.ProtoReflect.Descriptor instead.
func (*TaskContext) Descriptor() ([]byte, []int) {
	return file_task_proto_rawDescGZIP(), []int{1}
}

func (x *TaskContext) GetCwd() string {
	if x != nil && x.Cwd != nil {
		return *x.Cwd
	}
	return ""
}

func (x *TaskContext) GetTaskVariables() map[string]string {
	if x != nil {
		return x.TaskVariables
	}
	return nil
}

func (x *TaskContext) GetProjectEnv() map[string]string {
	if x != nil {
		return x.ProjectEnv
	}
	return nil
}

type Shell struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// Types that are assignable to ShellType:
	//
	//	*Shell_System
	//	*Shell_Program
	//	*Shell_WithArguments_
	ShellType isShell_ShellType `protobuf_oneof:"shell_type"`
}

func (x *Shell) Reset() {
	*x = Shell{}
	if protoimpl.UnsafeEnabled {
		mi := &file_task_proto_msgTypes[2]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Shell) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Shell) ProtoMessage() {}

func (x *Shell) ProtoReflect() protoreflect.Message {
	mi := &file_task_proto_msgTypes[2]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Shell.ProtoReflect.Descriptor instead.
func (*Shell) Descriptor() ([]byte, []int) {
	return file_task_proto_rawDescGZIP(), []int{2}
}

func (m *Shell) GetShellType() isShell_ShellType {
	if m != nil {
		return m.ShellType
	}
	return nil
}

func (x *Shell) GetSystem() *System {
	if x, ok := x.GetShellType().(*Shell_System); ok {
		return x.System
	}
	return nil
}

func (x *Shell) GetProgram() string {
	if x, ok := x.GetShellType().(*Shell_Program); ok {
		return x.Program
	}
	return ""
}

func (x *Shell) GetWithArguments() *Shell_WithArguments {
	if x, ok := x.GetShellType().(*Shell_WithArguments_); ok {
		return x.WithArguments
	}
	return nil
}

type isShell_ShellType interface {
	isShell_ShellType()
}

type Shell_System struct {
	System *System `protobuf:"bytes,1,opt,name=system,proto3,oneof"`
}

type Shell_Program struct {
	Program string `protobuf:"bytes,2,opt,name=program,proto3,oneof"`
}

type Shell_WithArguments_ struct {
	WithArguments *Shell_WithArguments `protobuf:"bytes,3,opt,name=with_arguments,json=withArguments,proto3,oneof"`
}

func (*Shell_System) isShell_ShellType() {}

func (*Shell_Program) isShell_ShellType() {}

func (*Shell_WithArguments_) isShell_ShellType() {}

type System struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields
}

func (x *System) Reset() {
	*x = System{}
	if protoimpl.UnsafeEnabled {
		mi := &file_task_proto_msgTypes[3]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *System) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*System) ProtoMessage() {}

func (x *System) ProtoReflect() protoreflect.Message {
	mi := &file_task_proto_msgTypes[3]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use System.ProtoReflect.Descriptor instead.
func (*System) Descriptor() ([]byte, []int) {
	return file_task_proto_rawDescGZIP(), []int{3}
}

type Shell_WithArguments struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	Program string   `protobuf:"bytes,1,opt,name=program,proto3" json:"program,omitempty"`
	Args    []string `protobuf:"bytes,2,rep,name=args,proto3" json:"args,omitempty"`
}

func (x *Shell_WithArguments) Reset() {
	*x = Shell_WithArguments{}
	if protoimpl.UnsafeEnabled {
		mi := &file_task_proto_msgTypes[7]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *Shell_WithArguments) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Shell_WithArguments) ProtoMessage() {}

func (x *Shell_WithArguments) ProtoReflect() protoreflect.Message {
	mi := &file_task_proto_msgTypes[7]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Shell_WithArguments.ProtoReflect.Descriptor instead.
func (*Shell_WithArguments) Descriptor() ([]byte, []int) {
	return file_task_proto_rawDescGZIP(), []int{2, 0}
}

func (x *Shell_WithArguments) GetProgram() string {
	if x != nil {
		return x.Program
	}
	return ""
}

func (x *Shell_WithArguments) GetArgs() []string {
	if x != nil {
		return x.Args
	}
	return nil
}

var File_task_proto protoreflect.FileDescriptor

var file_task_proto_rawDesc = []byte{
	0x0a, 0x0a, 0x74, 0x61, 0x73, 0x6b, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x0c, 0x7a, 0x65,
	0x64, 0x2e, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x73, 0x1a, 0x0c, 0x62, 0x75, 0x66, 0x66,
	0x65, 0x72, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0x8d, 0x02, 0x0a, 0x16, 0x54, 0x61, 0x73,
	0x6b, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x46, 0x6f, 0x72, 0x4c, 0x6f, 0x63, 0x61, 0x74,
	0x69, 0x6f, 0x6e, 0x12, 0x1d, 0x0a, 0x0a, 0x70, 0x72, 0x6f, 0x6a, 0x65, 0x63, 0x74, 0x5f, 0x69,
	0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x04, 0x52, 0x09, 0x70, 0x72, 0x6f, 0x6a, 0x65, 0x63, 0x74,
	0x49, 0x64, 0x12, 0x32, 0x0a, 0x08, 0x6c, 0x6f, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x18, 0x02,
	0x20, 0x01, 0x28, 0x0b, 0x32, 0x16, 0x2e, 0x7a, 0x65, 0x64, 0x2e, 0x6d, 0x65, 0x73, 0x73, 0x61,
	0x67, 0x65, 0x73, 0x2e, 0x4c, 0x6f, 0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x52, 0x08, 0x6c, 0x6f,
	0x63, 0x61, 0x74, 0x69, 0x6f, 0x6e, 0x12, 0x5e, 0x0a, 0x0e, 0x74, 0x61, 0x73, 0x6b, 0x5f, 0x76,
	0x61, 0x72, 0x69, 0x61, 0x62, 0x6c, 0x65, 0x73, 0x18, 0x03, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x37,
	0x2e, 0x7a, 0x65, 0x64, 0x2e, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x73, 0x2e, 0x54, 0x61,
	0x73, 0x6b, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x46, 0x6f, 0x72, 0x4c, 0x6f, 0x63, 0x61,
	0x74, 0x69, 0x6f, 0x6e, 0x2e, 0x54, 0x61, 0x73, 0x6b, 0x56, 0x61, 0x72, 0x69, 0x61, 0x62, 0x6c,
	0x65, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x0d, 0x74, 0x61, 0x73, 0x6b, 0x56, 0x61, 0x72,
	0x69, 0x61, 0x62, 0x6c, 0x65, 0x73, 0x1a, 0x40, 0x0a, 0x12, 0x54, 0x61, 0x73, 0x6b, 0x56, 0x61,
	0x72, 0x69, 0x61, 0x62, 0x6c, 0x65, 0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03,
	0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14,
	0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x76,
	0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38, 0x01, 0x22, 0xce, 0x02, 0x0a, 0x0b, 0x54, 0x61, 0x73,
	0x6b, 0x43, 0x6f, 0x6e, 0x74, 0x65, 0x78, 0x74, 0x12, 0x15, 0x0a, 0x03, 0x63, 0x77, 0x64, 0x18,
	0x01, 0x20, 0x01, 0x28, 0x09, 0x48, 0x00, 0x52, 0x03, 0x63, 0x77, 0x64, 0x88, 0x01, 0x01, 0x12,
	0x53, 0x0a, 0x0e, 0x74, 0x61, 0x73, 0x6b, 0x5f, 0x76, 0x61, 0x72, 0x69, 0x61, 0x62, 0x6c, 0x65,
	0x73, 0x18, 0x02, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x2c, 0x2e, 0x7a, 0x65, 0x64, 0x2e, 0x6d, 0x65,
	0x73, 0x73, 0x61, 0x67, 0x65, 0x73, 0x2e, 0x54, 0x61, 0x73, 0x6b, 0x43, 0x6f, 0x6e, 0x74, 0x65,
	0x78, 0x74, 0x2e, 0x54, 0x61, 0x73, 0x6b, 0x56, 0x61, 0x72, 0x69, 0x61, 0x62, 0x6c, 0x65, 0x73,
	0x45, 0x6e, 0x74, 0x72, 0x79, 0x52, 0x0d, 0x74, 0x61, 0x73, 0x6b, 0x56, 0x61, 0x72, 0x69, 0x61,
	0x62, 0x6c, 0x65, 0x73, 0x12, 0x4a, 0x0a, 0x0b, 0x70, 0x72, 0x6f, 0x6a, 0x65, 0x63, 0x74, 0x5f,
	0x65, 0x6e, 0x76, 0x18, 0x03, 0x20, 0x03, 0x28, 0x0b, 0x32, 0x29, 0x2e, 0x7a, 0x65, 0x64, 0x2e,
	0x6d, 0x65, 0x73, 0x73, 0x61, 0x67, 0x65, 0x73, 0x2e, 0x54, 0x61, 0x73, 0x6b, 0x43, 0x6f, 0x6e,
	0x74, 0x65, 0x78, 0x74, 0x2e, 0x50, 0x72, 0x6f, 0x6a, 0x65, 0x63, 0x74, 0x45, 0x6e, 0x76, 0x45,
	0x6e, 0x74, 0x72, 0x79, 0x52, 0x0a, 0x70, 0x72, 0x6f, 0x6a, 0x65, 0x63, 0x74, 0x45, 0x6e, 0x76,
	0x1a, 0x40, 0x0a, 0x12, 0x54, 0x61, 0x73, 0x6b, 0x56, 0x61, 0x72, 0x69, 0x61, 0x62, 0x6c, 0x65,
	0x73, 0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75,
	0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02,
	0x38, 0x01, 0x1a, 0x3d, 0x0a, 0x0f, 0x50, 0x72, 0x6f, 0x6a, 0x65, 0x63, 0x74, 0x45, 0x6e, 0x76,
	0x45, 0x6e, 0x74, 0x72, 0x79, 0x12, 0x10, 0x0a, 0x03, 0x6b, 0x65, 0x79, 0x18, 0x01, 0x20, 0x01,
	0x28, 0x09, 0x52, 0x03, 0x6b, 0x65, 0x79, 0x12, 0x14, 0x0a, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65,
	0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x05, 0x76, 0x61, 0x6c, 0x75, 0x65, 0x3a, 0x02, 0x38,
	0x01, 0x42, 0x06, 0x0a, 0x04, 0x5f, 0x63, 0x77, 0x64, 0x22, 0xec, 0x01, 0x0a, 0x05, 0x53, 0x68,
	0x65, 0x6c, 0x6c, 0x12, 0x2e, 0x0a, 0x06, 0x73, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x18, 0x01, 0x20,
	0x01, 0x28, 0x0b, 0x32, 0x14, 0x2e, 0x7a, 0x65, 0x64, 0x2e, 0x6d, 0x65, 0x73, 0x73, 0x61, 0x67,
	0x65, 0x73, 0x2e, 0x53, 0x79, 0x73, 0x74, 0x65, 0x6d, 0x48, 0x00, 0x52, 0x06, 0x73, 0x79, 0x73,
	0x74, 0x65, 0x6d, 0x12, 0x1a, 0x0a, 0x07, 0x70, 0x72, 0x6f, 0x67, 0x72, 0x61, 0x6d, 0x18, 0x02,
	0x20, 0x01, 0x28, 0x09, 0x48, 0x00, 0x52, 0x07, 0x70, 0x72, 0x6f, 0x67, 0x72, 0x61, 0x6d, 0x12,
	0x4a, 0x0a, 0x0e, 0x77, 0x69, 0x74, 0x68, 0x5f, 0x61, 0x72, 0x67, 0x75, 0x6d, 0x65, 0x6e, 0x74,
	0x73, 0x18, 0x03, 0x20, 0x01, 0x28, 0x0b, 0x32, 0x21, 0x2e, 0x7a, 0x65, 0x64, 0x2e, 0x6d, 0x65,
	0x73, 0x73, 0x61, 0x67, 0x65, 0x73, 0x2e, 0x53, 0x68, 0x65, 0x6c, 0x6c, 0x2e, 0x57, 0x69, 0x74,
	0x68, 0x41, 0x72, 0x67, 0x75, 0x6d, 0x65, 0x6e, 0x74, 0x73, 0x48, 0x00, 0x52, 0x0d, 0x77, 0x69,
	0x74, 0x68, 0x41, 0x72, 0x67, 0x75, 0x6d, 0x65, 0x6e, 0x74, 0x73, 0x1a, 0x3d, 0x0a, 0x0d, 0x57,
	0x69, 0x74, 0x68, 0x41, 0x72, 0x67, 0x75, 0x6d, 0x65, 0x6e, 0x74, 0x73, 0x12, 0x18, 0x0a, 0x07,
	0x70, 0x72, 0x6f, 0x67, 0x72, 0x61, 0x6d, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x07, 0x70,
	0x72, 0x6f, 0x67, 0x72, 0x61, 0x6d, 0x12, 0x12, 0x0a, 0x04, 0x61, 0x72, 0x67, 0x73, 0x18, 0x02,
	0x20, 0x03, 0x28, 0x09, 0x52, 0x04, 0x61, 0x72, 0x67, 0x73, 0x42, 0x0c, 0x0a, 0x0a, 0x73, 0x68,
	0x65, 0x6c, 0x6c, 0x5f, 0x74, 0x79, 0x70, 0x65, 0x22, 0x08, 0x0a, 0x06, 0x53, 0x79, 0x73, 0x74,
	0x65, 0x6d, 0x2a, 0x33, 0x0a, 0x0e, 0x52, 0x65, 0x76, 0x65, 0x61, 0x6c, 0x53, 0x74, 0x72, 0x61,
	0x74, 0x65, 0x67, 0x79, 0x12, 0x10, 0x0a, 0x0c, 0x52, 0x65, 0x76, 0x65, 0x61, 0x6c, 0x41, 0x6c,
	0x77, 0x61, 0x79, 0x73, 0x10, 0x00, 0x12, 0x0f, 0x0a, 0x0b, 0x52, 0x65, 0x76, 0x65, 0x61, 0x6c,
	0x4e, 0x65, 0x76, 0x65, 0x72, 0x10, 0x01, 0x2a, 0x40, 0x0a, 0x0c, 0x48, 0x69, 0x64, 0x65, 0x53,
	0x74, 0x72, 0x61, 0x74, 0x65, 0x67, 0x79, 0x12, 0x0e, 0x0a, 0x0a, 0x48, 0x69, 0x64, 0x65, 0x41,
	0x6c, 0x77, 0x61, 0x79, 0x73, 0x10, 0x00, 0x12, 0x0d, 0x0a, 0x09, 0x48, 0x69, 0x64, 0x65, 0x4e,
	0x65, 0x76, 0x65, 0x72, 0x10, 0x01, 0x12, 0x11, 0x0a, 0x0d, 0x48, 0x69, 0x64, 0x65, 0x4f, 0x6e,
	0x53, 0x75, 0x63, 0x63, 0x65, 0x73, 0x73, 0x10, 0x02, 0x42, 0x06, 0x5a, 0x04, 0x2e, 0x2f, 0x70,
	0x62, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var (
	file_task_proto_rawDescOnce sync.Once
	file_task_proto_rawDescData = file_task_proto_rawDesc
)

func file_task_proto_rawDescGZIP() []byte {
	file_task_proto_rawDescOnce.Do(func() {
		file_task_proto_rawDescData = protoimpl.X.CompressGZIP(file_task_proto_rawDescData)
	})
	return file_task_proto_rawDescData
}

var file_task_proto_enumTypes = make([]protoimpl.EnumInfo, 2)
var file_task_proto_msgTypes = make([]protoimpl.MessageInfo, 8)
var file_task_proto_goTypes = []interface{}{
	(RevealStrategy)(0),            // 0: zed.messages.RevealStrategy
	(HideStrategy)(0),              // 1: zed.messages.HideStrategy
	(*TaskContextForLocation)(nil), // 2: zed.messages.TaskContextForLocation
	(*TaskContext)(nil),            // 3: zed.messages.TaskContext
	(*Shell)(nil),                  // 4: zed.messages.Shell
	(*System)(nil),                 // 5: zed.messages.System
	nil,                            // 6: zed.messages.TaskContextForLocation.TaskVariablesEntry
	nil,                            // 7: zed.messages.TaskContext.TaskVariablesEntry
	nil,                            // 8: zed.messages.TaskContext.ProjectEnvEntry
	(*Shell_WithArguments)(nil),    // 9: zed.messages.Shell.WithArguments
	(*Location)(nil),               // 10: zed.messages.Location
}
var file_task_proto_depIdxs = []int32{
	10, // 0: zed.messages.TaskContextForLocation.location:type_name -> zed.messages.Location
	6,  // 1: zed.messages.TaskContextForLocation.task_variables:type_name -> zed.messages.TaskContextForLocation.TaskVariablesEntry
	7,  // 2: zed.messages.TaskContext.task_variables:type_name -> zed.messages.TaskContext.TaskVariablesEntry
	8,  // 3: zed.messages.TaskContext.project_env:type_name -> zed.messages.TaskContext.ProjectEnvEntry
	5,  // 4: zed.messages.Shell.system:type_name -> zed.messages.System
	9,  // 5: zed.messages.Shell.with_arguments:type_name -> zed.messages.Shell.WithArguments
	6,  // [6:6] is the sub-list for method output_type
	6,  // [6:6] is the sub-list for method input_type
	6,  // [6:6] is the sub-list for extension type_name
	6,  // [6:6] is the sub-list for extension extendee
	0,  // [0:6] is the sub-list for field type_name
}

func init() { file_task_proto_init() }
func file_task_proto_init() {
	if File_task_proto != nil {
		return
	}
	file_buffer_proto_init()
	if !protoimpl.UnsafeEnabled {
		file_task_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*TaskContextForLocation); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_task_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*TaskContext); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_task_proto_msgTypes[2].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Shell); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_task_proto_msgTypes[3].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*System); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_task_proto_msgTypes[7].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*Shell_WithArguments); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	file_task_proto_msgTypes[1].OneofWrappers = []interface{}{}
	file_task_proto_msgTypes[2].OneofWrappers = []interface{}{
		(*Shell_System)(nil),
		(*Shell_Program)(nil),
		(*Shell_WithArguments_)(nil),
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_task_proto_rawDesc,
			NumEnums:      2,
			NumMessages:   8,
			NumExtensions: 0,
			NumServices:   0,
		},
		GoTypes:           file_task_proto_goTypes,
		DependencyIndexes: file_task_proto_depIdxs,
		EnumInfos:         file_task_proto_enumTypes,
		MessageInfos:      file_task_proto_msgTypes,
	}.Build()
	File_task_proto = out.File
	file_task_proto_rawDesc = nil
	file_task_proto_goTypes = nil
	file_task_proto_depIdxs = nil
}
