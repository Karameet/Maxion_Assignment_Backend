package user_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"order-backend/internal/auth"
	"order-backend/internal/user"
	"order-backend/internal/user/memory"
)

// recordingRepo wraps the memory repo and captures what the service hands to
// storage, so tests can prove plaintext never reaches the repository.
type recordingRepo struct {
	*memory.Repo
	lastHash string
	calls    int
}

func (r *recordingRepo) CreateEmailUser(ctx context.Context, email, passwordHash string) (*user.User, error) {
	r.calls++
	r.lastHash = passwordHash
	return r.Repo.CreateEmailUser(ctx, email, passwordHash)
}

func (r *recordingRepo) UpsertByDeviceID(ctx context.Context, deviceID string) (*user.User, error) {
	r.calls++
	return r.Repo.UpsertByDeviceID(ctx, deviceID)
}

var hasher = auth.NewPasswordHasher(bcrypt.MinCost)

func newSvc() (*user.Service, *recordingRepo) {
	repo := &recordingRepo{Repo: memory.NewRepo()}
	return user.NewService(repo, hasher), repo
}

func TestLoginGuest_DeviceIDLength(t *testing.T) {
	cases := []struct {
		n       int
		wantErr bool
	}{{15, true}, {16, false}, {128, false}, {129, true}}
	for _, tc := range cases {
		svc, repo := newSvc()
		_, err := svc.LoginGuest(context.Background(), strings.Repeat("d", tc.n))
		if tc.wantErr {
			if !errors.Is(err, user.ErrInvalidDeviceID) {
				t.Fatalf("len %d: want ErrInvalidDeviceID, got %v", tc.n, err)
			}
			if repo.calls != 0 {
				t.Fatalf("len %d: repo must not be called on invalid input", tc.n)
			}
		} else if err != nil {
			t.Fatalf("len %d: unexpected err %v", tc.n, err)
		}
	}
}

func TestLoginGuest_SameDeviceSameUser(t *testing.T) {
	svc, _ := newSvc()
	a, _ := svc.LoginGuest(context.Background(), "device-0000000000001")
	b, _ := svc.LoginGuest(context.Background(), "device-0000000000001")
	if a.ID != b.ID {
		t.Fatalf("same device must map to same user: %v vs %v", a.ID, b.ID)
	}
	if a.AccountType() != user.AccountGuest {
		t.Fatalf("want guest, got %s", a.AccountType())
	}
}

func TestNormalizeEmail(t *testing.T) {
	good := map[string]string{
		"  A@B.com ":         "a@b.com",
		"Player@Example.com": "player@example.com",
	}
	for in, want := range good {
		got, err := user.NormalizeEmail(in)
		if err != nil || got != want {
			t.Fatalf("NormalizeEmail(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"", "   ", "not-an-email", "a@", "@b.com", "Name <a@b.com>", strings.Repeat("a", 250) + "@b.com"}
	for _, in := range bad {
		if _, err := user.NormalizeEmail(in); !errors.Is(err, user.ErrInvalidEmail) {
			t.Fatalf("NormalizeEmail(%q): want ErrInvalidEmail, got %v", in, err)
		}
	}
}

func TestRegister_PasswordLength(t *testing.T) {
	cases := []struct {
		n       int
		wantErr bool
	}{{7, true}, {8, false}, {72, false}, {73, true}}
	for _, tc := range cases {
		svc, repo := newSvc()
		_, err := svc.Register(context.Background(), "p@example.com", strings.Repeat("x", tc.n))
		if tc.wantErr {
			if !errors.Is(err, user.ErrWeakPassword) {
				t.Fatalf("len %d: want ErrWeakPassword, got %v", tc.n, err)
			}
			if repo.calls != 0 {
				t.Fatalf("len %d: repo must not be called", tc.n)
			}
		} else if err != nil {
			t.Fatalf("len %d: unexpected err %v", tc.n, err)
		}
	}
}

func TestRegister_StoresHashNotPlaintext(t *testing.T) {
	svc, repo := newSvc()
	const pw = "correct horse battery"
	u, err := svc.Register(context.Background(), "  Player@Example.com ", pw)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if repo.lastHash == pw || !strings.HasPrefix(repo.lastHash, "$2a$") {
		t.Fatalf("repo received non-bcrypt value: %q", repo.lastHash)
	}
	if !hasher.Compare(repo.lastHash, pw) {
		t.Fatal("stored hash must verify against the password")
	}
	if *u.Email != "player@example.com" || u.AccountType() != user.AccountEmail {
		t.Fatalf("unexpected user: email=%v type=%s", *u.Email, u.AccountType())
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	svc, _ := newSvc()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "dup@example.com", "password123"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := svc.Register(ctx, "DUP@example.com ", "password456"); !errors.Is(err, user.ErrEmailTaken) {
		t.Fatalf("want ErrEmailTaken, got %v", err)
	}
}

func TestLoginEmail(t *testing.T) {
	svc, _ := newSvc()
	ctx := context.Background()
	reg, err := svc.Register(ctx, "login@example.com", "password123")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	u, err := svc.LoginEmail(ctx, " LOGIN@example.com", "password123")
	if err != nil || u.ID != reg.ID {
		t.Fatalf("login: got %v, %v", u, err)
	}

	_, errWrongPw := svc.LoginEmail(ctx, "login@example.com", "wrong-password")
	_, errNoUser := svc.LoginEmail(ctx, "nobody@example.com", "password123")
	_, errBadEmail := svc.LoginEmail(ctx, "not-an-email", "password123")
	for name, e := range map[string]error{"wrong pw": errWrongPw, "no user": errNoUser, "bad email": errBadEmail} {
		if !errors.Is(e, user.ErrInvalidCredentials) {
			t.Fatalf("%s: want ErrInvalidCredentials, got %v", name, e)
		}
	}
}

func TestLinkEmail(t *testing.T) {
	svc, _ := newSvc()
	ctx := context.Background()
	guest, _ := svc.LoginGuest(ctx, "device-link-000000001")

	linked, err := svc.LinkEmail(ctx, guest.ID, "link@example.com", "password123")
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if linked.ID != guest.ID || linked.AccountType() != user.AccountEmail {
		t.Fatalf("link must keep id and become email account: %+v", linked)
	}
	if _, err := svc.LinkEmail(ctx, guest.ID, "other@example.com", "password123"); !errors.Is(err, user.ErrAlreadyLinked) {
		t.Fatalf("second link: want ErrAlreadyLinked, got %v", err)
	}

	other, _ := svc.LoginGuest(ctx, "device-link-000000002")
	if _, err := svc.LinkEmail(ctx, other.ID, "link@example.com", "password123"); !errors.Is(err, user.ErrEmailTaken) {
		t.Fatalf("taken email: want ErrEmailTaken, got %v", err)
	}

	if _, err := svc.LoginEmail(ctx, "link@example.com", "password123"); err != nil {
		t.Fatalf("login after link: %v", err)
	}
}
