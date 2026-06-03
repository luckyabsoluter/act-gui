package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type StartJobPayload struct {
	Name  string   `json:"name"`
	Needs []string `json:"needs,omitempty"`
}

func workflowJobsFromArgs(args []string) []StartJobPayload {
	workdir := firstFlagValue(args, "directory", "C")
	if workdir == "" {
		workdir = "."
	}

	workflow := firstFlagValue(args, "workflows", "W")
	if workflow == "" {
		workflow = filepath.Join(".github", "workflows")
	}
	if !filepath.IsAbs(workflow) {
		workflow = filepath.Join(workdir, workflow)
	}
	return workflowJobsFromPath(workflow)
}

func workflowJobsFromPath(path string) []StartJobPayload {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return workflowJobsFromFile(path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext == ".yml" || ext == ".yaml" {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	jobs := []StartJobPayload{}
	seen := map[string]bool{}
	for _, name := range names {
		for _, job := range workflowJobsFromFile(filepath.Join(path, name)) {
			if seen[job.Name] {
				continue
			}
			jobs = append(jobs, job)
			seen[job.Name] = true
		}
	}
	return jobs
}

func workflowJobsFromFile(path string) []StartJobPayload {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return workflowJobsFromContent(content)
}

func workflowJobsFromContent(content []byte) []StartJobPayload {
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil
	}
	root := documentRoot(&doc)
	jobsNode := mappingValue(root, "jobs")
	if jobsNode == nil || jobsNode.Kind != yaml.MappingNode {
		return nil
	}

	jobs := make([]StartJobPayload, 0, len(jobsNode.Content)/2)
	for i := 0; i+1 < len(jobsNode.Content); i += 2 {
		key := jobsNode.Content[i]
		value := jobsNode.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Value == "" {
			continue
		}
		jobs = append(jobs, StartJobPayload{
			Name:  key.Value,
			Needs: yamlStringList(mappingValue(value, "needs")),
		})
	}
	return jobs
}

func documentRoot(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return node.Content[0]
	}
	return node
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind == yaml.ScalarNode && node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func yamlStringList(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value == "" {
			return nil
		}
		return []string{node.Value}
	case yaml.SequenceNode:
		values := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				values = append(values, item.Value)
			}
		}
		return values
	default:
		return nil
	}
}

func encodeNeeds(needs []string) string {
	return strings.Join(needs, ",")
}

func decodeNeeds(needs string) []string {
	if needs == "" {
		return []string{}
	}
	parts := strings.Split(needs, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}
