/*
 * Copyright 2019 Brightgate Inc.
 *
 * This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at https://mozilla.org/MPL/2.0/.
 */


package cfgtree

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// Possible reasons for a Get operation to fail
var (
	ErrNoProp  = errors.New("no such property")
	ErrExpired = errors.New("property expired")
	ErrNotLeaf = errors.New("not a leaf node")
)

// PTree represents an in-core configuration tree, on which operations may be
// performed
type PTree struct {
	root *PNode
	path string

	cacheable     bool
	cachedMarshal map[string]*string

	preserved []*PNode // nodes preserved to allow for a rollback

	sync.Mutex
}

// PNode represents a single node (either internal or leaf) in a configuration
// tree.
type PNode struct {
	Value    string            `json:"Value,omitempty"`
	Modified *time.Time        `json:"Modified,omitempty"`
	Expires  *time.Time        `json:"Expires,omitempty"`
	Children map[string]*PNode `json:"Children,omitempty"`

	tree   *PTree
	parent *PNode
	name   string
	path   string
	hash   []byte
	data   interface{}

	cached     bool
	cacheScore int

	// As changes are made to nodes in the tree, copies of the original
	// nodes are preserved in this path->PNode map.  These copies are freed
	// when changes are committed, or are used to recover when a changeset
	// fails and must be reverted.
	origChildren map[string]*PNode
	origHash     []byte
}

// PFlat is a single node in a flat representation of a configuration tree.
type PFlat struct {
	Value    string
	Modified time.Time
	Expires  time.Time
	Hash     []byte
	Leaf     bool
}

func (t *PTree) parse(prop string) []string {
	if !strings.HasPrefix(prop, t.path) {
		return nil
	}

	body := strings.TrimPrefix(prop, t.path)
	body = strings.TrimSuffix(body, "/")

	x := strings.Split(body, "/")
	y := make([]string, 0)
	for _, z := range x {
		if len(z) > 0 {
			y = append(y, z)
		}
	}

	return y

}

// Tree returns the tree to which this property node belongs.
func (node *PNode) Tree() *PTree {
	return node.tree
}

// Name returns the name of this property node.
func (node *PNode) Name() string {
	return node.name
}

// Path returns the full path of this node.
func (node *PNode) Path() string {
	return node.path
}

// Parent returns this node's parent PNode.  If the subject is the root PNode,
// this function returns nil
func (node *PNode) Parent() *PNode {
	return node.parent
}

// SetData allows the cfgtree consumer to attach some arbitrary bit of data to a
// PNode.  This data is not persisted when the tree is exported.
func (node *PNode) SetData(d interface{}) {
	node.data = d
}

// Data returns whichever data (if any) was previously attached to this node
// with a SetData() operation.
func (node *PNode) Data() interface{} {
	return node.data
}

func hashCopy(in []byte) []byte {
	out := make([]byte, len(in))
	copy(out, in)
	return out
}

// Hash returns the previously computed hash value for this node
func (node *PNode) Hash() []byte {
	return hashCopy(node.hash)
}

// Rehash recomputes the hash value associated with this node
func (node *PNode) Rehash() []byte {
	if len(node.Children) == 0 {
		hash := md5.Sum([]byte(node.path + ":" + node.Value))
		if !bytes.Equal(hash[:], node.hash) {
			fmt.Printf("bad hash on leaf %s / %s", node.path, node.Value)
		}
		return hash[:]
	}

	hash := make([]byte, md5.Size)
	for _, c := range node.Children {
		chash := c.Rehash()
		for i := 0; i < md5.Size; i++ {
			hash[i] ^= chash[i]
		}
	}
	if !bytes.Equal(hash[:], node.hash) {
		fmt.Printf("bad hash on internal %s", node.path)
	}

	return hash
}

// Validate checks to be sure that the hash value we have stored matches the
// value we get if we recompute the hash.
func (node *PNode) Validate() bool {
	return bytes.Equal(node.hash, node.Rehash())
}

func (node *PNode) hashSelf() {
	if len(node.Children) == 0 {
		hash := md5.Sum([]byte(node.path + ":" + node.Value))
		node.hash = hash[:]
	} else {
		node.hash = make([]byte, md5.Size)
		for _, c := range node.Children {
			if c.hash == nil {
				log.Fatalf("missing hash for %s\n", c.path)
			}
			for i := 0; i < md5.Size; i++ {
				node.hash[i] ^= c.hash[i]
			}
		}
	}
}

func (node *PNode) hashDown() {
	for _, c := range node.Children {
		c.hashDown()
	}
	node.hashSelf()
}

func (node *PNode) hashUp() {
	node.hashSelf()
	if p := node.parent; p != nil {
		p.hashUp()
	}
}

func (node *PNode) moveChildren(path string) {
	node.path = path + "/" + node.name

	for _, child := range node.Children {
		child.moveChildren(node.path)
	}
}

// Move moves a node from one location in the tree to another, recomputing all
// of the hash values that may have been affected.  This is an unusual operation
// that is only likely to be invoked in the process of an upgrade that makes
// changes to the structure of the tree.
func (node *PNode) Move(newPath string) error {
	x, err := node.tree.insert(newPath)
	if err != nil {
		return err
	}
	x.hashUp()

	oldParent := node.parent
	delete(oldParent.Children, node.name)
	oldParent.hashUp()

	newParent := x.parent
	newParent.Children[x.name] = node
	node.parent = newParent
	node.name = x.name
	node.moveChildren(newPath)
	node.path = newPath
	node.hashDown()
	node.hashUp()

	return nil
}

func (node *PNode) subtree() []string {
	all := []string{node.path}
	for _, c := range node.Children {
		all = append(all, c.subtree()...)
	}

	return all
}

func (node *PNode) preserveChildren() {
	tree := node.tree

	if tree.preserved == nil {
		log.Fatalf("making changes outside of a changeset")
	}

	if node.origChildren == nil {

		node.origChildren = make(map[string]*PNode)
		tree.preserved = append(tree.preserved, node)
		for k, v := range node.Children {
			node.origChildren[k] = v
		}
	}
}

func (node *PNode) preserve() {
	nodeCopy := *node

	p := node.parent
	p.preserveChildren()
	if p.origChildren[node.name] == node {
		p.origChildren[node.name] = &nodeCopy
	}

	for x := p; x != nil; x = x.parent {
		if x.origHash == nil {
			x.origHash = hashCopy(x.hash)
		}
	}
}

func (node *PNode) update(value string, exp *time.Time) error {
	var err error

	if len(node.Children) > 0 {
		err = fmt.Errorf("can only modify leaf properties")
	} else if node.parent == nil {
		err = fmt.Errorf("attempted to modify the root node")
	} else {
		node.preserve()
		node.Value = value
		node.Expires = exp
	}

	return err
}

func (node *PNode) uncache(recurse bool) {
	if node.cached {
		node.cached = false
		node.cacheScore = 0
		delete(node.tree.cachedMarshal, node.path)
	}
	if recurse {
		for _, child := range node.Children {
			child.uncache(true)
		}
	}
}

func (node *PNode) commit(now time.Time) bool {
	updated := false

	// Look for any children that have been added or updated
	for prop, current := range node.Children {
		if old := node.origChildren[prop]; old != current {
			if old == nil || current.Value != old.Value ||
				current.Expires != old.Expires {
				copy := now
				current.Modified = &copy
				current.uncache(false)
				updated = true
			}
		}
	}

	// Using the original list of child nodes, look for any that have been
	// deleted
	for prop, origNode := range node.origChildren {
		if _, ok := node.Children[prop]; !ok {
			origNode.uncache(true)
			updated = true
		}
	}

	// Set the modified time for all nodes between here and the root.  Clean
	// up any preserved hashes as well.
	for x := node; x != nil; x = x.parent {
		x.origHash = nil
		if updated {
			copy := now
			x.Modified = &copy
			x.uncache(false)
		}
	}
	return updated
}

// ChangesetInit prepares the tree to have a series of operations performed,
// which need to be accepted or rejected as an atomic unit.  When this call
// returns, the tree will remain locked until the changeset is committed or
// abandoned.
func (t *PTree) ChangesetInit() {
	t.Lock()
	if t.preserved != nil {
		log.Fatalf("attempting to nest changesets")
	}

	t.preserved = make([]*PNode, 0)
}

// ChangesetCommit accepts all changes that have been made to the tree since
// ChangesetInit() was called.  Any relevant update/delete hooks attached to the
// tree will be invoked now.
func (t *PTree) ChangesetCommit() bool {
	now := time.Now()
	updated := false

	// Iterate over all of the nodes that were preserved, looking to see
	// whether any of them have been changed.
	for _, node := range t.preserved {
		if node.commit(now) {
			updated = true
		}
		node.origChildren = nil
	}

	t.preserved = nil
	t.Unlock()

	return updated
}

// ChangesetRevert will revert any changes that were made to the tree since
// ChangesetInit() was called.
func (t *PTree) ChangesetRevert() {
	for _, node := range t.preserved {
		// restore the original children
		node.Children = node.origChildren
		node.origChildren = nil

		// restore the original hash values
		for x := node; x != nil; x = x.parent {
			if x.origHash != nil {
				x.hash = x.origHash
			}
		}
	}
	t.preserved = nil
	t.Unlock()
}

/*
 * Insert an empty property into the tree, returning the leaf node.  If the
 * property already exists, the tree is left unchanged.  If the node exists, but
 * is not a leaf, return an error.
 */
func (t *PTree) insert(prop string) (*PNode, error) {
	var err error

	components := t.parse(prop)
	if components == nil || len(components) < 1 {
		return nil, fmt.Errorf("invalid property path: %s", prop)
	}

	node := t.root
	path := t.path
	for _, name := range components {
		if node.Children == nil {
			node.Children = make(map[string]*PNode)
		}
		path += "/" + name
		next := node.Children[name]
		if next == nil {
			node.preserveChildren()
			next = &PNode{
				tree:   t,
				name:   name,
				parent: node,
				path:   path,
				data:   nil,
			}

			node.Children[name] = next
		}
		node = next
	}

	if node != nil && len(node.Children) > 0 {
		err = fmt.Errorf("inserting an internal node: %s", prop)
	}

	return node, err
}

func (t *PTree) search(prop string) *PNode {
	components := t.parse(prop)
	if components == nil {
		return nil
	}

	node := t.root
	ok := false
	for _, name := range components {
		if node, ok = node.Children[name]; !ok {
			break
		}
	}

	// If the caller explicitly asked for an internal node and we found a
	// leaf, don't operate on it.
	if node != nil && len(node.Children) == 0 && strings.HasSuffix(prop, "/") {
		node = nil
	}

	return node
}

// Delete will delete a PNode from the config tree, along with any children of
// that node.  On success, it returns a slice containing all of the nodes
// deleted from the tree.
func (t *PTree) Delete(prop string) ([]string, error) {
	node := t.search(prop)
	if node == nil {
		return nil, ErrNoProp
	}

	if parent := node.parent; parent != nil {
		parent.preserveChildren()
		delete(parent.Children, node.name)
		parent.hashUp()
	}
	all := node.subtree()

	return all, nil
}

// Root returns the root PNode of the config tree
func (t *PTree) Root() *PNode {
	return t.root
}

// GetProp will return the property of a leaf node indicated by the provided
// property path
func (t *PTree) GetProp(prop string) (string, error) {
	var val string

	n, err := t.GetNode(prop)
	if err == nil {
		if len(n.Children) == 0 {
			val = n.Value
		} else {
			err = ErrNotLeaf
		}
	}

	return val, err
}

// GetNode will return the node indicated by the provided property path
func (t *PTree) GetNode(prop string) (*PNode, error) {
	var err error

	node := t.search(prop)

	if node == nil {
		err = ErrNoProp
	} else if node.Expired() {
		err = ErrExpired
	}

	return node, err
}

// GetChildren retrieves the properties subtree rooted at the given property,
// and returns a map representing the immediate children, if any, of that
// property.  It is not considered an error if the property is missing or
// has no children.
func (t *PTree) GetChildren(prop string) map[string]*PNode {
	var rval map[string]*PNode

	if node, err := t.GetNode(prop); err == nil {
		rval = node.Children
	}

	return rval
}

// SetCacheable is a hint that this tree is stable enough that we should
// consider caching the results of marshaling frequently used subtrees.
func (t *PTree) SetCacheable() {
	t.cacheable = true
	t.cachedMarshal = make(map[string]*string)
}

// Get will find the node indicated by the provided path, and will return a
// marshaled JSON structure representing the node, or the subtree rooted at
// that node.
func (t *PTree) Get(prop string) (*string, error) {
	var rval *string

	node, err := t.GetNode(prop)
	if err != nil {
		return nil, err
	}

	if t.cacheable {
		if rval, ok := t.cachedMarshal[node.path]; ok {
			return rval, nil
		}
	}

	node.cacheScore++
	b, err := json.Marshal(node)
	if err == nil {
		x := string(b)
		rval = &x
		if t.cacheable && node.cacheScore > 2 {
			node.cached = true
			t.cachedMarshal[node.path] = rval
		}
	}

	return rval, err
}

func (t *PTree) addset(prop string, val string, exp *time.Time, add bool) error {
	var node *PNode
	var err error

	if add {
		node, err = t.insert(prop)
	} else if node = t.search(prop); node == nil {
		err = fmt.Errorf("no such property: %s", prop)
	}
	if node != nil {
		err = node.update(val, exp)
		if err == nil {
			node.hashUp()
		}
	}
	return err

}

// Add will add a single property to the tree
func (t *PTree) Add(prop string, val string, exp *time.Time) error {
	return t.addset(prop, val, exp, true)
}

// Set will update a single property
func (t *PTree) Set(prop string, val string, exp *time.Time) error {
	return t.addset(prop, val, exp, false)
}

// Export will return a JSON-marshaled representation of the entire config tree,
// which may be used to either persist the tree or send it across a network.
func (t *PTree) Export(humanize bool) []byte {
	var j []byte
	var err error
	if humanize {
		j, err = json.MarshalIndent(t.root, "", "  ")
	} else {
		j, err = json.Marshal(t.root)
	}
	if err != nil {
		log.Fatalf("Failed to construct properties JSON: %v\n", err)
	}
	return j
}

// After loading the initial property values, we need to walk the tree to set
// the parent pointers, as well as the node and path fields.
func (t *PTree) patch(node *PNode, name string, path string) {
	if len(path) > 0 {
		path += "/"
	}
	path = path + name

	node.path = path
	node.name = name
	node.tree = t

	for childName, child := range node.Children {
		child.parent = node
		t.patch(child, childName, path)
	}
	node.hashSelf()
}

// Replace will replace the complete contents of a config tree
func (t *PTree) Replace(data []byte) error {
	var newRoot PNode

	if err := json.Unmarshal(data, &newRoot); err != nil {
		return errors.Wrap(err, "unmarshaling properties")
	}
	t.root = &newRoot
	t.patch(t.root, t.path, "")

	// You can't roll back from a full tree replacement.  It's up to the
	// caller to ensure that the replacement is not part of a compound
	// operation that may need rolling back.
	t.preserved = nil

	return nil
}

// Expired returns true if the property has an expiration time which has already
// passed.
func (node *PNode) Expired() bool {
	if node.Expires == nil {
		return false
	}
	if node.Expires.After(time.Now()) {
		return false
	}

	return true
}

func (node *PNode) dump(w io.Writer, indent string) {
	e := ""
	if node.Expires != nil {
		e = node.Expires.Format("2006-01-02T15:04:05")
	}
	fmt.Fprintf(w, "%s%s: %s  %s\n", indent, node.Name(), node.Value, e)
	for _, child := range node.Children {
		child.dump(w, indent+"  ")
	}
}

// Dump will dump the whole tree to stdout
func (t *PTree) Dump(w io.Writer) {
	t.root.dump(w, "")
}

func (node *PNode) flattenNode(flat *map[string]*PFlat, internal bool) {
	if len(node.Children) == 0 || internal {
		fnode := PFlat{
			Value: node.Value,
			Leaf:  len(node.Children) == 0,
			Hash:  node.Hash(),
		}
		if node.Modified != nil {
			fnode.Modified = *node.Modified
		}
		if node.Expires != nil {
			fnode.Expires = *node.Expires
		}

		(*flat)[node.path] = &fnode
	}
	for _, c := range node.Children {
		c.flattenNode(flat, internal)
	}
}

func (t *PTree) flattenTree(internal bool) map[string]*PFlat {
	flat := make(map[string]*PFlat)
	t.root.flattenNode(&flat, internal)

	return flat
}

// FlattenLeaves returns a flattened version of the leaf nodes of the provided
// tree.
func (t *PTree) FlattenLeaves() map[string]*PFlat {
	return t.flattenTree(false)
}

// Flatten returns a flattened version of the provided tree, including both leaf
// and internal nodes
func (t *PTree) Flatten() map[string]*PFlat {
	return t.flattenTree(true)
}

// GraftTree will finalize a partially instantiated tree.  It will compute the
// hashes, generate the 'path' fields, and set the parent pointers for each
// node.
func GraftTree(path string, root *PNode) *PTree {
	path = strings.TrimSuffix(path, "/")
	t := PTree{
		root: root,
		path: path,
	}
	t.patch(root, path, "")

	return &t
}

// NewPTree will accept a JSON-marshaled representation of a config tree, and
// will return a PTree structure that can be operated upon.
func NewPTree(path string, data []byte) (*PTree, error) {
	var newRoot PNode

	if !strings.HasPrefix(path, "@/") || !strings.HasSuffix(path, "/") {
		return nil, fmt.Errorf("invalid path: %s", path)
	}

	if data != nil {
		err := json.Unmarshal(data, &newRoot)
		if err != nil {
			return nil, errors.Wrap(err, "unmarshaling properties")
		}
	}

	return GraftTree(path, &newRoot), nil
}

