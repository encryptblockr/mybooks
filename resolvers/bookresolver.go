package resolvers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexsergivan/mybooks/services"

	"github.com/alexsergivan/mybooks/flash"

	"gorm.io/gorm/clause"

	"github.com/alexsergivan/mybooks/book"

	"github.com/alexsergivan/mybooks/userbook"
	"github.com/labstack/echo/v4"
	"github.com/wader/gormstore/v2"
	"gorm.io/gorm"
)

func RateBookForm(db *gorm.DB, storage *gormstore.Store) echo.HandlerFunc {
	return func(c echo.Context) error {

		uID := GetUserIdFromSession(c, storage)

		u := userbook.GetUserByID(int64(*uID), db)
		bookID := c.QueryParam("book")

		var bookRating *userbook.BookRating
		var bookItem *userbook.Book
		if uID != nil && bookID != "" {
			br := userbook.GetUserBookRating(int64(*uID), bookID, db)
			if br.BookID != "" && br.Book.Title != "" {
				bookRating = &br
				bookItem = &bookRating.Book
			} else {
				b := userbook.GetBookByID(bookID, db)
				if b.ID != "" {
					bookItem = &b
				}
			}

		} else {
			bookRating = nil
		}

		if u.Email != nil {
			return c.Render(http.StatusOK, "book--rate", map[string]interface{}{
				"profile":    u,
				"bookRating": bookRating,
				"bookID":     bookID,
				"book":       bookItem,
			})
		}
		return c.Redirect(http.StatusSeeOther, "/")
	}
}

func RateBookSubmit(db *gorm.DB, storage *gormstore.Store, bookApiService *book.BooksApi) echo.HandlerFunc {
	return func(c echo.Context) error {
		bookID := c.FormValue("bookID")
		bookFromApi, err := bookApiService.GetBook(bookID)
		if err != nil {
			flash.SetFlashMessage(c, flash.MessageTypeError, `Please try to select a book again`)
			return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("rateBook"))
		}

		if bookFromApi.ServerResponse.HTTPStatusCode != 200 {
			flash.SetFlashMessage(c, flash.MessageTypeError, fmt.Sprintf(`Something went wrong: %d`, bookFromApi.ServerResponse.HTTPStatusCode))
			return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("rateBook"))
		}
		// Prepare authors.
		var authors []*userbook.Author
		if len(bookFromApi.VolumeInfo.Authors) > 0 {
			for _, author := range bookFromApi.VolumeInfo.Authors {
				authors = append(authors, &userbook.Author{
					Name: author,
				})
			}
		}

		var category string
		if len(bookFromApi.VolumeInfo.Categories) > 0 {
			// Use 1st category.
			category = bookFromApi.VolumeInfo.Categories[0]
		} else {
			category = "No Category"
		}

		uID := GetUserIdFromSession(c, storage)
		if uID == nil {
			flash.SetFlashMessage(c, flash.MessageTypeError, `Your session expired. Please Sign In again.`)
			return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("rateBook"))
		}
		rate, _ := strconv.ParseFloat(c.FormValue("rate"), 64)
		image, thumbnail := "", ""
		if bookFromApi.VolumeInfo.ImageLinks.Large != "" {
			image = strings.Replace(bookFromApi.VolumeInfo.ImageLinks.Large, "http://", "https://", -1)
		} else {
			if bookFromApi.VolumeInfo.ImageLinks.Medium != "" {
				image = strings.Replace(bookFromApi.VolumeInfo.ImageLinks.Medium, "http://", "https://", -1)
			}
		}
		if bookFromApi.VolumeInfo.ImageLinks.Thumbnail != "" {
			thumbnail = strings.Replace(bookFromApi.VolumeInfo.ImageLinks.Thumbnail, "http://", "https://", -1)
		}

		if image != "" {
			image, err = services.SaveBookCover(image, bookID, "large")
			if err != nil {
				log.Println(err)
			}
		}
		if thumbnail != "" {
			thumbnail, err = services.SaveBookCover(thumbnail, bookID, "thumbnail")
			if err != nil {
				log.Println(err)
			}
		}

		rating := userbook.BookRating{
			Book: userbook.Book{
				ID:            bookID,
				Title:         bookFromApi.VolumeInfo.Title,
				Subtitle:      bookFromApi.VolumeInfo.Subtitle,
				PublishedDate: bookFromApi.VolumeInfo.PublishedDate,
				GoogleID:      bookID,
				Authors:       authors,
				Description:   template.HTML(bookFromApi.VolumeInfo.Description),
				Category: userbook.Category{
					Name: category,
				},
				Thumbnail: thumbnail,
				Image:     image,
			},
			User: userbook.User{
				Model: gorm.Model{
					ID: *uID,
				},
			},
			Rate:      rate,
			Comment:   c.FormValue("comment"),
			CreatedAt: time.Now(),
		}

		db.Clauses(clause.OnConflict{
			UpdateAll: true,
		}).Create(&rating)

		if rating.BookID != "" {
			flash.SetFlashMessage(c, flash.MessageTypeMessage, fmt.Sprintf(`Your review of "%s" have been published!`, bookFromApi.VolumeInfo.Title))
		}

		return c.Redirect(http.StatusSeeOther, c.Echo().Reverse("userHome"))
	}
}

func BookProfilePage(db *gorm.DB, storage *gormstore.Store) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("id")
		book := userbook.GetBookByID(id, db)
		if book.Title != "" {
			pageSize := 15
			ratingsCount := userbook.GetBookRatingsCount(book.ID, db)

			page, _ := strconv.Atoi(c.QueryParam("page"))
			if page == 0 {
				page++
			}

			var nextPage int
			if page*pageSize < int(ratingsCount) {
				nextPage = page + 1
			}

			avgRating := userbook.GetAverageBookRating(book.ID, db)

			stars := services.ConvertRateFrom100To5(avgRating)

			return c.Render(http.StatusOK, "book--profile", map[string]interface{}{
				"book":      book,
				"ratings":   userbook.GetBookRatings(book.ID, db.Scopes(services.Paginate(c, pageSize))),
				"nextPage":  nextPage,
				"rate":      userbook.GetAverageBookRating(book.ID, db),
				"stars":     stars,
				"rateCount": ratingsCount,
			})
		} else {
			return c.Redirect(http.StatusSeeOther, "/")
		}
	}
}

func BooksPage(db *gorm.DB, storage *gormstore.Store) echo.HandlerFunc {
	return func(c echo.Context) error {
		pageSize := 15
		booksCount := userbook.GetBooksCount(db)

		page, _ := strconv.Atoi(c.QueryParam("page"))
		if page == 0 {
			page++
		}

		var nextPage int
		if page*pageSize < int(booksCount) {
			nextPage = page + 1
		}
		b := userbook.GetBooksWithRating(db, c, pageSize)
		return c.Render(http.StatusOK, "books--books", map[string]interface{}{
			"books":    b,
			"nextPage": nextPage,
		})
	}
}

func BooksSearchAutocomplete(db *gorm.DB, storage *gormstore.Store) echo.HandlerFunc {
	return func(c echo.Context) error {
		books := userbook.GetBooks(db.Scopes(BookTitleLike(c, c.QueryParam("q"))))
		var booksItems []*userbook.Book
		for _, volume := range books {
			if volume != nil {
				booksItems = append(booksItems, volume)
			}
		}

		return c.JSON(http.StatusOK, booksItems)
	}
}

func BookTitleLike(c echo.Context, title string) func(db *gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where("title LIKE ?", "%"+title+"%")
	}
}