package controller

import (
	"context"
	"fmt"
	"golang-restaurant-management/database"
	helper "golang-restaurant-management/helpers"
	"golang-restaurant-management/models"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

var userCollection *mongo.Collection = database.OpenCollection(database.Client, "user")

func GetUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		recordPerPage, err := strconv.Atoi(c.Query("recordPerPage"))

		if err != nil || recordPerPage < 1 {
			recordPerPage = 10
		}

		page, err1 := strconv.Atoi(c.Query("page"))

		if err1 != nil || page < 1 {
			page = 1
		}

		startIndex := (page - 1) * recordPerPage
		startIndex, err = strconv.Atoi(c.Query("startIndex"))

		matchStage := bson.D{{"$match", bson.D{{}}}}
		projectStage := bson.D{
			{
				"$project", bson.D{
					{"_id", 0},
					{"total_count", 1},
					{"user_items", bson.D{{"$slice", []interface{}{"$data", startIndex, recordPerPage}}}},
				}}}

		result, err := userCollection.Aggregate(ctx, mongo.Pipeline{
			matchStage, projectStage})
		defer cancel()

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error occured while listing user items"})
		}

		// either pass an error
		// ideally want to return all the users based on various query parameters

		var allUsers []bson.M

		if err = result.All(ctx, &allUsers); err != nil {
			log.Fatal(err)
		}

		c.JSON(http.StatusOK, allUsers)
	}
}

func GetUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		userId := c.Param("user_id")

		var user models.User

		err := userCollection.FindOne(ctx, bson.M{"user_id": userId}).Decode(&user)

		defer cancel()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, user)
	}
}

func SignUp() gin.HandlerFunc {
	return func(c *gin.Context) {

		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		var user models.User

		// convert the JSON data coming from FE to something that golang understands

		if err := c.BindJSON(&user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// validate the data based on user struct

		validationError := validate.Struct(user)

		if validationError != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": validationError.Error()})
			return
		}

		// we will check whether the "email" has already been used by another user or not

		count, err := userCollection.CountDocuments(ctx, bson.M{"email": user.Email})
		defer cancel()

		if err != nil {
			log.Panic(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error occured while checking for the mail"})
			return
		}

		// hash the password - HashPassword()

		password := HashPassword(*user.Password)
		user.Password = &password

		// we will check whether the "phone_number" has already been used by another user or not

		count, err = userCollection.CountDocuments(ctx, bson.M{"phone": user.Phone})
		defer cancel()

		if err != nil {
			log.Panic(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error occured while checking for the phone number"})
			return
		}

		if count > 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "this email or phone number already exists"})
			return
		}

		// create some extra details for the user object - basically fillers (created_at, updated_at and ID)

		user.Created_at, _ = time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))
		user.Updated_at, _ = time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))
		user.ID = primitive.NewObjectID()
		user.User_id = user.ID.Hex()

		// generate token and refresh_token (GenerateAllTokens function from helper)

		token, refresh_token, _ := helper.GenerateAllTokens(*user.Email, *user.First_name, *user.Last_name, user.User_id)
		user.Token = &token
		user.Refresh_token = &refresh_token

		// if all ok, then we insert the new user into the user collecion

		resultInsertionNumber, insertErr := userCollection.InsertOne(ctx, user)

		if insertErr != nil {
			msg := fmt.Sprintf("User item was not created")
			c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
			return
		}

		// return status OK and send the result back

		defer cancel()
		c.JSON(http.StatusOK, resultInsertionNumber)

	}
}

func Login() gin.HandlerFunc {
	return func(c *gin.Context) {

		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		var user models.User
		var foundUser models.User

		// convert the login data from postman which is in JSON to golang readable format

		if err := c.BindJSON(&user); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// find a user with the email and see if the user even exists

		err := userCollection.FindOne(ctx, bson.M{"email": user.Email}).Decode(&foundUser)
		defer cancel()

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "user not found, login seems to be incorrect"})
			return
		}

		// then you will verify the password - VerifyPassword()

		passwordIsValid, msg := VerifyPassword(*user.Password, *foundUser.Password)
		defer cancel()

		if passwordIsValid != true {
			c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
			return
		}

		// if all goes well, then we will generate tokens

		token, refreshToken, _ := helper.GenerateAllTokens(*foundUser.Email, *foundUser.First_name, *foundUser.Last_name, foundUser.User_id)

		// update tokens - token and refresh_token

		helper.UpdateAllTokens(token, refreshToken, foundUser.User_id)

		// return StatusOK

		c.JSON(http.StatusOK, foundUser)
	}
}

func HashPassword(password string) string {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		log.Panic(err)
	}

	return string(bytes)
}

func VerifyPassword(userPassword string, providedPassword string) (bool, string) {
	err := bcrypt.CompareHashAndPassword([]byte(providedPassword), []byte(userPassword))
	check := true
	msg := ""

	if err != nil {
		msg = fmt.Sprintf("login or password is incorrect")
		check = false
	}

	return check, msg
}
