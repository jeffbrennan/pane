package main

import "strings"

type pickerNode struct {
	name, path     string
	file           *fileEntry
	children       []*pickerNode
	byName         map[string]*pickerNode
	added, deleted int
}

type pickerItem struct {
	path, name     string
	branch         string
	directory      bool
	collapsed      bool
	file           fileEntry
	added, deleted int
}

func pickerTree(entries []fileEntry, collapsed map[string]bool) []pickerItem {
	root := &pickerNode{}
	for _, entry := range entries {
		parts := strings.Split(entry.path, "/")
		node := root
		for i, part := range parts {
			child := node.byName[part]
			if child == nil {
				child = &pickerNode{name: part, path: strings.Join(parts[:i+1], "/")}
				node.children = append(node.children, child)
				if node.byName == nil {
					node.byName = make(map[string]*pickerNode)
				}
				node.byName[part] = child
			}
			child.added += entry.added
			child.deleted += entry.deleted
			if i == len(parts)-1 {
				file := entry
				child.file = &file
			}
			node = child
		}
	}
	var items []pickerItem
	var appendChildren func(*pickerNode, string)
	appendChildren = func(parent *pickerNode, prefix string) {
		for i, node := range parent.children {
			last := i == len(parent.children)-1
			branch, childPrefix := "├─ ", prefix+"│  "
			if last {
				branch, childPrefix = "└─ ", prefix+"   "
			}
			item := pickerItem{path: node.path, name: node.name, branch: prefix + branch, directory: node.file == nil, collapsed: collapsed[node.path], added: node.added, deleted: node.deleted}
			if node.file != nil {
				item.file = *node.file
			}
			items = append(items, item)
			if item.directory && !item.collapsed {
				appendChildren(node, childPrefix)
			}
		}
	}
	appendChildren(root, "")
	return items
}
