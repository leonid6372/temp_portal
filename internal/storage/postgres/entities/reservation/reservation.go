package reservation

import (
	"fmt"
	"log"
	"portal/internal/storage/postgres"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/lib/pq"
)

const (
	// Получение актуальных мест 1. делаем все места "доступно" и вычитаем занятые 2. прибавляем занятые с пометкой "недостпуно"
	qrGetActualPlaces = `(SELECT place_id, "name", COALESCE(phone, ''), true AS is_available, 0 AS user_id, TIMESTAMP '0001-01-01 00:00:00' AS start, TIMESTAMP '0001-01-01 00:00:00' AS finish FROM place
						  EXCEPT
						  SELECT DISTINCT place_id, "name", COALESCE(phone, ''), true AS is_available, 0, TIMESTAMP '0001-01-01 00:00:00', TIMESTAMP '0001-01-01 00:00:00' FROM place_and_reservation
						  WHERE ($1, $2) OVERLAPS ("start", finish))
						  UNION
						  (SELECT DISTINCT place_id, "name", COALESCE(phone, ''), false AS is_available, user_id, "start", finish FROM place_and_reservation
						  WHERE ($1, $2) OVERLAPS ("start", finish))
						  ORDER BY place_id;`
	qrGetReservationsByUserID       = `SELECT reservation_id, place_id, start, finish FROM reservation WHERE user_id = $1 ORDER BY start DESC;`
	qrGetUserReservationInDateRange = `SELECT reservation_id FROM reservation WHERE user_id = $1 AND (start, finish) OVERLAPS ($2, $3);`
	qrGetIsPlaceAvailable           = `SELECT reservation_id FROM reservation WHERE place_id = $1 AND (start, finish) OVERLAPS ($2, $3);`
	qrGetNameByPlaceID              = `SELECT name FROM place WHERE place_id = $1;`
	qrInsertReservation             = `INSERT INTO reservation (place_id, start, finish, user_id) VALUES ($1, $3, $4, $2);`
	qrUpdateReservation             = `UPDATE reservation SET place_id = $2, start = $3, finish = $4 WHERE reservation_id = $1;`
	qrDeleteReservation             = `DELETE FROM reservation WHERE reservation_id = $1;`
)

type Place struct {
	PlaceID      int    `json:"place_id,omitempty"`
	Name         string `json:"name,omitempty"`
	Phone        string `json:"phone,omitempty"`
	Internet     string `json:"internet,omitempty"`
	SecondScreen string `json:"second_screen,omitempty"`
}

func (p *Place) GetPlaceName(storage *postgres.Storage, placeID int) error {
	const op = "storage.postgres.entities.reservation.GetPlaceName"

	qrResult, err := storage.DB.Query(qrGetNameByPlaceID, placeID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer qrResult.Close()

	for qrResult.Next() {
		err = qrResult.Scan(&p.Name)
		if err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
	}

	return nil
}

type ActualPlace struct {
	Place
	IsAvailable bool `json:"is_available"`
	UserID      int  `json:"user_id"`
	Start       int  `json:"start"`
	Finish      int  `json:"finish"`
}

func (ap *ActualPlace) GetActualPlaces(storage *postgres.Storage, properties string, start, finish time.Time) ([]ActualPlace, error) {
	const op = "storage.postgres.entities.reservation.GetActualPlaces"

	qrResult, err := storage.DB.Query(qrGetActualPlaces, start, finish)
	if err != nil {
		if e, ok := err.(*pq.Error); ok {
			log.Print(e.Detail)
		}
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer qrResult.Close()

	var aps []ActualPlace
	var rawStart, rawFinish time.Time

	for qrResult.Next() {
		var ap ActualPlace
		if err := qrResult.Scan(&ap.PlaceID, &ap.Name, &ap.Phone, &ap.IsAvailable, &ap.UserID, &rawStart, &rawFinish); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		ap.Start = int(rawStart.UnixMilli())
		ap.Finish = int(rawFinish.UnixMilli())
		aps = append(aps, ap)
	}

	return aps, nil
}

type Reservation struct {
	ReservationID int              `json:"reservation_id,omitempty"`
	PlaceID       int              `json:"place_id,omitempty"`
	Start         pgtype.Timestamp `json:"start,omitempty"`
	Finish        pgtype.Timestamp `json:"finish,omitempty"`
	UserID        int              `json:"user_id,omitempty"`
}

func (r *Reservation) HasUserReservationInDateRange(storage *postgres.Storage, userID int, start, finish string) (bool, error) {
	const op = "storage.postgres.entities.reservation.HasUserReservationInDateRange" // Имя текущей функции для логов и ошибок

	qrResult, err := storage.DB.Query(qrGetUserReservationInDateRange, userID, start, finish)
	if err != nil {
		return false, fmt.Errorf("%s: %w", op, err)
	}
	defer qrResult.Close()

	// Проверка на пустой ответ
	if !qrResult.Next() {
		return false, nil
	}

	return true, nil
}

func (r *Reservation) InsertReservation(storage *postgres.Storage, placeID, userID int, start, finish string) error {
	const op = "storage.postgres.entities.reservation.InsertReservation" // Имя текущей функции для логов и ошибок

	qrResult, err := storage.DB.Query(qrGetIsPlaceAvailable, placeID, start, finish)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	defer qrResult.Close()

	// Проверка на пустой ответ
	if qrResult.Next() {
		return fmt.Errorf("%s: place is already taken", op)
	}

	_, err = storage.DB.Exec(qrInsertReservation, placeID, userID, start, finish)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (r *Reservation) UpdateReservation(storage *postgres.Storage, reservationID, placeID int, start, finish time.Time) error {
	const op = "storage.postgres.entities.reservation.UpdateReservation" // Имя текущей функции для логов и ошибок

	_, err := storage.DB.Exec(qrUpdateReservation, reservationID, placeID, start, finish)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (r *Reservation) DeleteReservation(storage *postgres.Storage, reservationID int) error {
	const op = "storage.postgres.entities.reservation.DeleteReservation" // Имя текущей функции для логов и ошибок

	_, err := storage.DB.Exec(qrDeleteReservation, reservationID)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (r *Reservation) GetReservationsByUserID(storage *postgres.Storage, userID int) ([]Reservation, error) {
	const op = "storage.postgres.entities.reservation.GetReservationsByUserID" // Имя текущей функции для логов и ошибок

	qrResult, err := storage.DB.Query(qrGetReservationsByUserID, userID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer qrResult.Close()

	var rs []Reservation
	for qrResult.Next() {
		var r Reservation
		if err := qrResult.Scan(&r.ReservationID, &r.PlaceID, &r.Start, &r.Finish); err != nil {
			return nil, fmt.Errorf("%s: %w", op, err)
		}
		rs = append(rs, r)
	}

	return rs, nil
}
