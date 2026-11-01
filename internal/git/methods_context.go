package git

import "strings"

func (r Repo) FullContext(key Context, base, oldPath string) (string, error) {
	if strings.Contains(base, "\nnew file mode ") {
		return base, nil
	}
	paths := []string{key.Path}
	if oldPath != "" {
		paths = append(paths, oldPath)
	}
	out, err := r.Command(r.FullContextArgs(key, paths)...)
	if err != nil {
		return "", GitError(out, err)
	}
	return string(out), nil
}

func (r Repo) FullContextArgs(key Context, paths []string) []string {
	args := append([]string{"diff"}, r.diffOptions()...)
	args = append(args, "--unified=1000000")
	if key.Base {
		args = append(args, key.Commit)
	} else if key.Commit != "" {
		args = append([]string{"show", "--first-parent", "--format="}, r.diffOptions()...)
		args = append(args, "--unified=1000000", key.Commit)
	} else if key.Staged {
		args = append(args, "--cached")
	}
	return append(append(args, "--"), paths...)
}
