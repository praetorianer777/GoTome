package dedupe

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/praetorianer777/gotome/backend/internal/db"
	"github.com/praetorianer777/gotome/backend/internal/db/sqlc"
	"github.com/praetorianer777/gotome/backend/internal/jobs"
)

const (
	// Two texts overlap when they share at least minJaccard of all their
	// shingles, or one holds at least minContainment of the other's.
	minJaccard     = 0.5
	minContainment = 0.8
	// crowd is how many files a bucket may hold and still be followed.
	crowd = 50
)

// SignTx makes the signature of the book's primary text file from its
// chunks, in the transaction that wrote them, and drops the signatures of
// the book's other files.
func (s *Service) SignTx(ctx context.Context, tx pgx.Tx, bookID, fileID uuid.UUID) error {
	q := sqlc.New(tx)
	if err := q.DropBookSignatures(ctx, sqlc.DropBookSignaturesParams{BookID: bookID, FileID: fileID}); err != nil {
		return err
	}
	if err := q.DropFileBuckets(ctx, fileID); err != nil {
		return err
	}
	texts, err := q.ListFileChunkTexts(ctx, fileID)
	if err != nil {
		return err
	}
	sig, ok := Sign(texts)
	if !ok {
		return q.PutFileSignature(ctx, sqlc.PutFileSignatureParams{FileID: fileID, Version: SignatureVersion})
	}
	if err := q.PutFileSignature(ctx, sqlc.PutFileSignatureParams{
		FileID: fileID, Version: SignatureVersion, Shingles: int32(sig.Shingles), Signature: encode(sig.Values),
	}); err != nil {
		return err
	}
	put := sqlc.PutFileBucketsParams{FileID: fileID}
	for _, b := range sig.Buckets {
		put.Bands = append(put.Bands, b.Band)
		put.Buckets = append(put.Buckets, b.Key)
	}
	return q.PutFileBuckets(ctx, put)
}

// ChunkedTx follows the writing of a book's chunks: the file is signed and
// the book checked once the transaction has committed.
func (s *Service) ChunkedTx(ctx context.Context, tx pgx.Tx, bookID, fileID uuid.UUID) error {
	if err := s.SignTx(ctx, tx, bookID, fileID); err != nil {
		return err
	}
	return s.EnqueueTx(ctx, tx, bookID)
}

// overlaps is the evidence of shared text between the book's primary text
// file and other books', as Check records it.
func overlaps(ctx context.Context, q *sqlc.Queries, bookID uuid.UUID) ([]sqlc.FindDuplicateEvidenceRow, error) {
	mine, err := q.GetBookSignature(ctx, bookID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	others, err := q.ListSignatureCompanions(ctx, sqlc.ListSignatureCompanionsParams{
		FileID: mine.FileID, BookID: bookID, Crowd: crowd,
	})
	if err != nil {
		return nil, err
	}
	values := decode(mine.Signature)
	var out []sqlc.FindDuplicateEvidenceRow
	for _, o := range others {
		j := Jaccard(values, decode(o.Signature))
		in, holds := Containment(j, int(mine.Shingles), int(o.Shingles))
		if j < minJaccard && in < minContainment && holds < minContainment {
			continue
		}
		// The detail reads from the pair's first book to its second.
		if o.BookID.String() < bookID.String() {
			in, holds = holds, in
		}
		out = append(out, sqlc.FindDuplicateEvidenceRow{
			Other: o.BookID, Kind: EvidenceOverlap,
			Detail: fmt.Sprintf("jaccard=%.2f a_in_b=%.2f b_in_a=%.2f", j, in, holds),
		})
	}
	return out, nil
}

// EnqueueUnsigned asks for every chunked primary text file without a
// signature of this version to be signed. The app calls it at start.
func (s *Service) EnqueueUnsigned(ctx context.Context) error {
	files, err := sqlc.New(s.pool).ListUnsignedFiles(ctx, SignatureVersion)
	if err != nil || len(files) == 0 {
		return err
	}
	args := make([]river.JobArgs, len(files))
	for i, f := range files {
		args[i] = SignArgs{FileID: f.ID, BookID: f.BookID}
	}
	if _, err := s.Queue.InsertMany(ctx, args, jobs.InsertOpts{
		Queue: jobs.QueueDefault, Unique: true, MaxAttempts: checkAttempts,
	}); err != nil {
		return fmt.Errorf("queue signing: %w", err)
	}
	s.log.Info("queued the signing of texts for overlap", "files", len(files))
	return nil
}

// SignFile signs the file, if it is still its book's text, and checks the
// book.
func (s *Service) SignFile(ctx context.Context, bookID, fileID uuid.UUID) error {
	return db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		primary, err := sqlc.New(tx).GetPrimaryTextFile(ctx, bookID)
		if errors.Is(err, pgx.ErrNoRows) || primary == nil || *primary != fileID {
			return nil
		}
		if err != nil {
			return err
		}
		return s.ChunkedTx(ctx, tx, bookID, fileID)
	})
}

// SignArgs is the job that signs one file from its chunks.
type SignArgs struct {
	FileID uuid.UUID `json:"fileId"`
	BookID uuid.UUID `json:"bookId"`
}

// signKind names the job in the queue. It is stored with every job, so it
// stays.
const signKind = "dedupe.sign_file"

func (SignArgs) Kind() string { return signKind }

// SignWorker runs SignArgs.
type SignWorker struct {
	river.WorkerDefaults[SignArgs]
	Service *Service
}

func (w *SignWorker) Work(ctx context.Context, job *river.Job[SignArgs]) error {
	return w.Service.SignFile(ctx, job.Args.BookID, job.Args.FileID)
}
