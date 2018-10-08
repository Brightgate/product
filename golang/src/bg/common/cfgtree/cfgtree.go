/*
 * COPYRIGHT 2018 Brightgate Inc.  All rights reserved.
 *
 * This copyright notice is Copyright Management Information under 17 USC 1202
 * and is included to protect this work and deter copyright infringement.
 * Removal or alteration of this Copyright Management Information without the
 * express written permission of Brightgate Inc is prohibited, and any
 * such unauthorized removal or alteration will be a violation of federal law.
 */

package cfgtree

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Possible reasons for a Get operation to fail
var (
	ErrNoProp  = errors.New("no such property")
	ErrExpired = errors.New("property expired")
)

// PTree represents an in-core configuration tree, on which operations may be
// performed
type PTree struct {
	root      *PNode
	callbacks Callbacks

	rollback struct {
		preserved []*PNode
		deleted   []*PNode
	}

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

	// As changes are made to nodes in the tree, copies of the original
	// nodes are preserved in this path->PNode map.  These copies are freed
	// when changes are committed, or are used to recover when a changeset
	// fails and must be reverted.
	preserved map[string]*PNode
}

// Callbacks allow cfgtree consumers to provide hooks that may be invoked when a
// property is updated or deleted.
type Callbacks struct {
	Updated func(node *PNode)
	Deleted func(node *PNode)
}

var nullCallbacks = Callbacks{
	Updated: func(n *PNode) {},
	Deleted: func(n *PNode) {},
}

func parse(prop string) []string {
	prop = strings.TrimSuffix(prop, "/")
	if prop == "@" {
		return make([]string, 0)
	}

	/*
	 * Only accept properties that start with exactly one '@', meaning they
	 * are local to this device
	 */
	if len(prop) < 2 || prop[0] != '@' || prop[1] != '/' {
		return nil
	}

	x := strings.Split(prop[2:], "/")
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

// Hash returns the previously computed hash value for this node
func (node *PNode) Hash() []byte {
	return node.hash
}

// Rehash recomputes the hash value associated with this node
func (node *PNode) Rehash() []byte {
	if len(node.Children) == 0 {
		hash := md5.Sum([]byte(node.path + ":" + node.Value))
		return hash[:]
	}

	hash := make([]byte, md5.Size)
	for _, c := range node.Children {
		chash := c.Rehash()
		for i := 0; i < md5.Size; i++ {
			hash[i] ^= chash[i]
		}
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
				// When committing a multi-step operation, we
				// can't update the parent hashes until all of
				// the children have been computed.  If we find
				// a child with no hash yet, bail out.  We'll
				// finish rehashing this node when we generate
				// that child's hash
				return
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
	return nil
}

func (node *PNode) delete() {
	if cb := node.tree.callbacks.Deleted; cb != nil {
		cb(node)
	}

	for _, n := range node.Children {
		n.delete()
	}
}

func (node *PNode) preserveChildren() {
	if node.preserved == nil {
		tree := node.tree

		node.preserved = make(map[string]*PNode)
		tree.rollback.preserved = append(tree.rollback.preserved, node)
		for k, v := range node.Children {
			node.preserved[k] = v
		}
	}
}

func (node *PNode) preserve() {
	nodeCopy := *node

	p := node.parent
	p.preserveChildren()
	p.preserved[node.name] = &nodeCopy
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

func (node *PNode) commit(now time.Time) bool {
	updated := false

	tree := node.tree

	// Look for any children that have been added or updated
	for prop, current := range node.Children {
		if old := node.preserved[prop]; old != current {
			if old == nil || current.Value != old.Value ||
				current.Expires != old.Expires {
				if cb := tree.callbacks.Updated; cb != nil {
					cb(current)
				}
				copy := now
				current.Modified = &copy
				updated = true
			}
		}
	}

	// Using the original list of child nodes, look for any that have been
	// deleted
	for prop := range node.preserved {
		if _, ok := node.Children[prop]; !ok {
			updated = true
		}
	}

	// If there have been any changes to child nodes, mark this and all
	// ancestors as updated
	if updated {
		node.hashDown()
		node.hashUp()
		for x := node; x != nil; x = x.parent {
			copy := now
			x.Modified = &copy
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
	t.rollback.preserved = make([]*PNode, 0)
	t.rollback.deleted = make([]*PNode, 0)
}

// ChangesetCommit accepts all changes that have been made to the tree since
// ChangesetInit() was called.  Any relevant update/delete hooks attached to the
// tree will be invoked now.
func (t *PTree) ChangesetCommit() bool {
	now := time.Now()
	updated := false

	// Iterate over all of the nodes that were preserved, looking to see
	// whether any of them have been changed.
	for _, node := range t.rollback.preserved {
		if node.commit(now) {
			updated = true
		}
		node.preserved = nil
	}

	// If any nodes were deleted, we need to clean up any associated
	// expiration state
	for _, node := range t.rollback.deleted {
		updated = true
		node.delete()
	}

	t.rollback.preserved = nil
	t.rollback.deleted = nil
	t.Unlock()

	return updated
}

// ChangesetRevert will revert any changes that were made to the tree since
// ChangesetInit() was called.
func (t *PTree) ChangesetRevert() {
	for _, node := range t.rollback.preserved {
		node.Children = node.preserved
		node.preserved = nil
	}
	t.rollback.preserved = nil
	t.rollback.deleted = nil
	t.Unlock()
}

/*
 * Insert an empty property into the tree, returning the leaf node.  If the
 * property already exists, the tree is left unchanged.  If the node exists, but
 * is not a leaf, return an error.
 */
func (t *PTree) insert(prop string) (*PNode, error) {
	var err error

	components := parse(prop)
	if components == nil || len(components) < 1 {
		return nil, fmt.Errorf("invalid property path: %s", prop)
	}

	node := t.root
	path := "@"
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
	components := parse(prop)
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
// that node.
func (t *PTree) Delete(prop string) error {
	node := t.search(prop)
	if node == nil {
		return fmt.Errorf("deleting a nonexistent property: %s", prop)
	}

	if parent := node.parent; parent != nil {
		parent.preserveChildren()
		delete(parent.Children, node.name)
	}
	t.rollback.deleted = append(t.rollback.deleted, node)

	return nil
}

// Root returns the root PNode of the config tree
func (t *PTree) Root() *PNode {
	return t.root
}

// GetNode will return the node indicated by the provided property path
func (t *PTree) GetNode(prop string) (*PNode, error) {
	var err error

	node := t.search(prop)

	if node == nil {
		err = ErrNoProp
	} else if node.Expires != nil && node.Expires.Before(time.Now()) {
		err = ErrExpired
	}

	return node, err
}

// Get will find the node indicated by the provided path, and will return a
// marshaled JSON structure representing the node, or the subtree rooted at
// that node.
func (t *PTree) Get(prop string) (string, error) {
	var rval string
	var b []byte

	node, err := t.GetNode(prop)
	if err == nil {
		if b, err = json.Marshal(node); err == nil {
			rval = string(b)
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
func (t *PTree) Export() []byte {
	s, err := json.MarshalIndent(t.root, "", "  ")
	if err != nil {
		log.Fatalf("Failed to construct properties JSON: %v\n", err)
	}
	return s
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

// RegisterCallbacks attaches a Callbacks structure to the tree
func (t *PTree) RegisterCallbacks(callbacks Callbacks) {
	t.callbacks = callbacks
}

// GraftTree will finalize a partially instantiated tree.  It will compute the
// hashes, generate the 'path' fields, and set the parent pointers for each
// node.
func GraftTree(path string, root *PNode) *PTree {
	t := PTree{
		root:      root,
		callbacks: nullCallbacks,
	}
	t.patch(root, path, "")

	return &t
}

// NewPTree will accept a JSON-marshaled representation of a config tree, and
// will return a PTree structure that can be operated upon.
func NewPTree(path string, data []byte) (*PTree, error) {
	var newRoot PNode

	if err := json.Unmarshal(data, &newRoot); err != nil {
		return nil, fmt.Errorf("unmarshalling properties")
	}

	return GraftTree(path, &newRoot), nil
}
