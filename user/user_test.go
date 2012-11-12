package user

import (
	"github.com/globocom/config"
	"github.com/globocom/gandalf/db"
	"github.com/globocom/gandalf/fs"
	"github.com/globocom/gandalf/key"
	"github.com/globocom/gandalf/repository"
	fstesting "github.com/globocom/tsuru/fs/testing"
	"io/ioutil"
	"labix.org/v2/mgo/bson"
	. "launchpad.net/gocheck"
	"os"
	"path"
	"testing"
)

func Test(t *testing.T) { TestingT(t) }

type S struct {
	rfs *fstesting.RecordingFs
}

var _ = Suite(&S{})

func (s *S) authKeysContent(c *C) string {
	authFile := path.Join(os.Getenv("HOME"), ".ssh", "authorized_keys")
	f, err := fs.Filesystem().OpenFile(authFile, os.O_RDWR, 0755)
	c.Assert(err, IsNil)
	b, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	return string(b)
}

func (s *S) SetUpSuite(c *C) {
	err := config.ReadConfigFile("../etc/gandalf.conf")
	c.Check(err, IsNil)
}

func (s *S) SetUpTest(c *C) {
	s.rfs = &fstesting.RecordingFs{}
	fs.Fsystem = s.rfs
}

func (s *S) TearDownSuite(c *C) {
	fs.Fsystem = nil
}

func (s *S) TestNewUserReturnsAStructFilled(c *C) {
	u, err := New("someuser", []key.Key{{Content: "id_rsa someKeyChars", Name: "somekey"}})
	c.Assert(err, IsNil)
	defer db.Session.User().Remove(bson.M{"_id": u.Name})
	c.Assert(u.Name, Equals, "someuser")
	c.Assert(len(u.Keys), Not(Equals), 0)
}

func (s *S) TestNewUserShouldStoreUserInDatabase(c *C) {
	u, err := New("someuser", []key.Key{{Content: "id_rsa someKeyChars", Name: "somekey"}})
	c.Assert(err, IsNil)
	defer db.Session.User().Remove(bson.M{"_id": u.Name})
	err = db.Session.User().FindId(u.Name).One(&u)
	c.Assert(err, IsNil)
	c.Assert(u.Name, Equals, "someuser")
	c.Assert(len(u.Keys), Not(Equals), 0)
}

func (s *S) TestNewChecksIfUserIsValidBeforeStoring(c *C) {
	_, err := New("", []key.Key{})
	c.Assert(err, NotNil)
	got := err.Error()
	expected := "Validation Error: user name is not valid"
	c.Assert(got, Equals, expected)
}

func (s *S) TestNewWritesKeyInAuthorizedKeys(c *C) {
	u, err := New("piccolo", []key.Key{{Content: "idrsakey piccolo@myhost", Name: "somekey"}})
	c.Assert(err, IsNil)
	defer db.Session.User().Remove(bson.M{"_id": u.Name})
	keys := s.authKeysContent(c)
	c.Assert(keys, Matches, ".*idrsakey piccolo@myhost")
}

func (s *S) TestIsValidReturnsErrorWhenUserDoesNotHaveAName(c *C) {
	u := User{Keys: []key.Key{{Content: "id_rsa foooBar", Name: "somekey"}}}
	v, err := u.isValid()
	c.Assert(v, Equals, false)
	c.Assert(err, NotNil)
	expected := "Validation Error: user name is not valid"
	got := err.Error()
	c.Assert(got, Equals, expected)
}

func (s *S) TestIsValidShouldNotAcceptEmptyUserName(c *C) {
	u := User{Keys: []key.Key{{Content: "id_rsa foooBar", Name: "somekey"}}}
	v, err := u.isValid()
	c.Assert(v, Equals, false)
	c.Assert(err, NotNil)
	expected := "Validation Error: user name is not valid"
	got := err.Error()
	c.Assert(got, Equals, expected)
}

func (s *S) TestIsValidShouldAcceptEmailsAsUserName(c *C) {
	u := User{Name: "r2d2@gmail.com", Keys: []key.Key{{Content: "id_rsa foooBar", Name: "somekey"}}}
	v, err := u.isValid()
	c.Assert(err, IsNil)
	c.Assert(v, Equals, true)
}

func (s *S) TestRemove(c *C) {
	u, err := New("someuser", []key.Key{})
	c.Assert(err, IsNil)
	err = Remove(u.Name)
	c.Assert(err, IsNil)
	lenght, err := db.Session.User().FindId(u.Name).Count()
	c.Assert(err, IsNil)
	c.Assert(lenght, Equals, 0)
}

func (s *S) TestRemoveRemovesKeyFromAuthorizedKeysFile(c *C) {
	u, err := New("gandalf", []key.Key{{Content: "gandalfkey gandalf@mordor", Name: "somekey"}})
	c.Assert(err, IsNil)
	err = Remove(u.Name)
	c.Assert(err, IsNil)
	got := s.authKeysContent(c)
	c.Assert(got, Not(Matches), ".*gandalfkey gandalf@mordor")
}

func (s *S) TestRemoveInexistentUserReturnsDescriptiveMessage(c *C) {
	err := Remove("otheruser")
	c.Assert(err, ErrorMatches, "Could not remove user: not found")
}

func (s *S) TestRemoveDoesNotRemovesUserWhenUserIsTheOnlyOneAssciatedWithOneRepository(c *C) {
	u, err := New("silver", []key.Key{})
	c.Assert(err, IsNil)
	r := s.createRepo("run", []string{u.Name}, c)
	defer db.Session.Repository().Remove(bson.M{"_id": r.Name})
	defer db.Session.User().Remove(bson.M{"_id": u.Name})
	err = Remove(u.Name)
	c.Assert(err, ErrorMatches, "^Could not remove user: user is the only one with access to at least one of it's repositories$")
}

func (s *S) TestRemoveRevokesAccessToReposWithMoreThanOneUserAssociated(c *C) {
	u, r, r2 := s.userPlusRepos(c)
	defer db.Session.Repository().Remove(bson.M{"_id": r.Name})
	defer db.Session.Repository().Remove(bson.M{"_id": r2.Name})
	defer db.Session.User().Remove(bson.M{"_id": u.Name})
	err := Remove(u.Name)
	c.Assert(err, IsNil)
	s.retrieveRepos(r, r2, c)
	c.Assert(r.Users, DeepEquals, []string{"slot"})
	c.Assert(r2.Users, DeepEquals, []string{"cnot"})
}

func (s *S) retrieveRepos(r, r2 *repository.Repository, c *C) {
	err := db.Session.Repository().FindId(r.Name).One(&r)
	c.Assert(err, IsNil)
	err = db.Session.Repository().FindId(r2.Name).One(&r2)
	c.Assert(err, IsNil)
}

func (s *S) userPlusRepos(c *C) (*User, *repository.Repository, *repository.Repository) {
	u, err := New("silver", []key.Key{})
	c.Assert(err, IsNil)
	r := s.createRepo("run", []string{u.Name, "slot"}, c)
	r2 := s.createRepo("stay", []string{u.Name, "cnot"}, c)
	return u, &r, &r2
}

func (s *S) createRepo(name string, users []string, c *C) repository.Repository {
	r := repository.Repository{Name: name, Users: users}
	err := db.Session.Repository().Insert(&r)
	c.Assert(err, IsNil)
	return r
}

func (s *S) TestHandleAssociatedRepositoriesShouldRevokeAccessToRepoWithMoreThanOneUserAssociated(c *C) {
	u, r, r2 := s.userPlusRepos(c)
	defer db.Session.Repository().RemoveId(r.Name)
	defer db.Session.Repository().RemoveId(r2.Name)
	defer db.Session.User().RemoveId(u.Name)
	err := u.handleAssociatedRepositories()
	c.Assert(err, IsNil)
	s.retrieveRepos(r, r2, c)
	c.Assert(r.Users, DeepEquals, []string{"slot"})
	c.Assert(r2.Users, DeepEquals, []string{"cnot"})
}

func (s *S) TestHandleAssociateRepositoriesReturnsErrorWhenUserIsOnlyOneWithAccessToAtLeastOneRepo(c *C) {
	u, err := New("umi", []key.Key{})
	c.Assert(err, IsNil)
	r := s.createRepo("proj1", []string{"umi"}, c)
	defer db.Session.User().RemoveId(u.Name)
	defer db.Session.Repository().RemoveId(r.Name)
	err = u.handleAssociatedRepositories()
	expected := "^Could not remove user: user is the only one with access to at least one of it's repositories$"
	c.Assert(err, ErrorMatches, expected)
}

func (s *S) TestAddKeyShouldAppendKeyIntoUsersDocument(c *C) {
	u, err := New("umi", []key.Key{})
	defer db.Session.User().RemoveId(u.Name)
	k := key.Key{Content: "ssh-rsa mykey umi@lolcats", Name: "somekey"}
	err = AddKey("umi", &k)
	c.Assert(err, IsNil)
	err = db.Session.User().FindId(u.Name).One(&u)
	c.Assert(u.Keys, DeepEquals, []key.Key{k})
}

func (s *S) TestAddKeyShouldWriteKeyInAuthorizedKeys(c *C) {
	u, err := New("umi", []key.Key{})
	defer db.Session.User().RemoveId(u.Name)
	k := key.Key{Content: "ssh-rsa mykey umi@lolcats", Name: "somekey"}
	err = AddKey("umi", &k)
	c.Assert(err, IsNil)
	content := s.authKeysContent(c)
	c.Assert(content, Matches, ".* "+k.Content)
}

func (s *S) TestAddKeyShouldReturnCustomErrorWhenUserDoesNotExists(c *C) {
	err := AddKey("umi", &key.Key{Content: "ssh-rsa mykey umi@host", Name: "somekey"})
	c.Assert(err, ErrorMatches, `^User "umi" not found$`)
}

func (s *S) TestRemoveKeyShouldRemoveKeyFromUserDocument(c *C) {
	u, err := New("luke", []key.Key{{Content: "ssh-rsa lukeskey@home", Name: "homekey"}})
	c.Assert(err, IsNil)
	defer db.Session.User().RemoveId(u.Name)
	err = RemoveKey("luke", "homekey")
	c.Assert(err, IsNil)
	err = db.Session.User().FindId(u.Name).One(&u)
	c.Assert(err, IsNil)
	c.Assert(u.Keys, DeepEquals, []key.Key{})
}

func (s *S) TestRemoveKeyShouldRemoveFromAuthorizedKeysFile(c *C) {
	k := "ssh-rsa lukeskey@home"
	u, err := New("luke", []key.Key{{Content: k, Name: "homekey"}})
	c.Assert(err, IsNil)
	defer db.Session.User().RemoveId(u.Name)
	err = RemoveKey("luke", "homekey")
	c.Assert(err, IsNil)
	content := s.authKeysContent(c)
	c.Assert(content, Not(Matches), ".* "+k)
}
