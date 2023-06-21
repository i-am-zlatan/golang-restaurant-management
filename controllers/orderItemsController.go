package controller

import (
	"context"
	"golang-restaurant-management/database"
	"golang-restaurant-management/models"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type OrderItemPack struct {
	Table_id    *string
	Order_items []models.OrderItem
}

var orderItemCollection *mongo.Collection = database.OpenCollection(database.Client, "orderItem")

func GetOrderItems() gin.HandlerFunc {
	return func(c *gin.Context) {
		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		result, err := orderItemCollection.Find(context.TODO(), bson.M{})

		defer cancel()

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error occured while listing order items"})
			return
		}
		var allOrderItems []bson.M

		if err = result.All(ctx, &allOrderItems); err != nil {
			log.Fatal(err)
			return
		}
		c.JSON(http.StatusOK, allOrderItems)
	}
}

func GetOrderItemsByOrder() gin.HandlerFunc {
	return func(c *gin.Context) {
		orderId := c.Param("order_id")

		allOrderItems, err := ItemsByOrder(orderId)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error occured while listing order items by order id"})
			return
		}

		c.JSON(http.StatusOK, allOrderItems)
	}
}

func ItemsByOrder(id string) (orderItems []primitive.M, err error) {
	var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

	matchStage := bson.D{{"$match", bson.D{{"order_id", id}}}}
	lookupStage := bson.D{{"$lookup", bson.D{{"from", "food"}, {"localField", "food_id"}, {"foreignField", "food_id"}, {"as", "food"}}}}
	unwindStage := bson.D{{"$unwind", bson.D{{"path", "$food"}, {"preserveNullAndEmptyArrays", true}}}}

	lookupOrderStage := bson.D{{"$lookup", bson.D{{"from", "order"}, {"localField", "order_id"}, {"foreignField", "order_id"}, {"as", "order"}}}}
	unwindOrderStage := bson.D{{"$unwind", bson.D{{"path", "$order"}, {"preserveNullAndEmptyArrays", true}}}}

	lookupTableStage := bson.D{{"$lookup", bson.D{{"from", "table"}, {"localField", "order.table_id"}, {"foreignField", "table_id"}, {"as", "table"}}}}
	unwindTableStage := bson.D{{"$unwind", bson.D{{"path", "$table"}, {"preserveNullAndEmptyArrays", true}}}}

	projectStage := bson.D{
		{
			"$project", bson.D{
				{"id", 0},
				{"amount", "$food.price"},
				{"total_count", 1},
				{"food_name", "$food.name"},
				{"food-image", "$food.food_image"},
				{"table_number", "$table.table_number"},
				{"table_id", "$table.table_id"},
				{"order_id", "$order.order_id"},
				{"price", "$food.price"},
				{"quantity", 1},
			}}}

	groupStage := bson.D{{"$group", bson.D{{"_id", bson.D{{"order_id", "$order_id"}, {"table_id", "$table_id"}, {"table_number", "$table_number"}}}, {"payment_due", bson.D{{"$sum", "$amount"}}}, {"total_count", bson.D{{"$sum", 1}}}, {"order_items", bson.D{{"$push", "$$ROOT"}}}}}}

	projectStage2 := bson.D{
		{
			"$project", bson.D{
				{"id", 0},
				{"payment_due", 1},
				{"total_count", 1},
				{"table_number", "$_id.table_number"},
				{"order_items", 1},
			}}}

	result, err := orderItemCollection.Aggregate(ctx, mongo.Pipeline{
		matchStage,
		lookupStage,
		unwindStage,
		lookupOrderStage,
		unwindOrderStage,
		lookupTableStage,
		unwindTableStage,
		projectStage,
		groupStage,
		projectStage2})

	if err != nil {
		panic(err)
	}
	if err = result.All(ctx, &orderItems); err != nil {
		panic(err)
	}

	defer cancel()

	return orderItems, err
}

func GetOrderItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		orderItemId := c.Param("order_item_id")
		var orderItem models.OrderItem

		err := orderItemCollection.FindOne(ctx, bson.M{"order_item_id": orderItemId}).Decode(&orderItem)
		defer cancel()

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "error occured while listing ordered item"})
			return
		}
		c.JSON(http.StatusOK, orderItem)
	}
}

func CreateOrderItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		var orderItemPack OrderItemPack
		var order models.Order

		if err := c.BindJSON(&orderItemPack); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		order.Order_date, _ = time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))

		orderItemsToBeInserted := []interface{}{}
		order.Table_id = orderItemPack.Table_id
		order_id := OrdetItemOrderCreator(order)

		for _, orderItem := range orderItemPack.Order_items {
			orderItem.Order_id = order_id

			validationError := validate.Struct(orderItem)

			if validationError != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": validationError.Error()})
				return
			}

			orderItem.ID = primitive.NewObjectID()
			orderItem.Created_at, _ = time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))
			orderItem.Updated_at, _ = time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))
			orderItem.Order_item_id = orderItem.ID.Hex()
			var num = toFixed(*orderItem.Unit_price, 2)
			orderItem.Unit_price = &num
			orderItemsToBeInserted = append(orderItemsToBeInserted, orderItem)
		}

		insertedOrderItems, err := orderItemCollection.InsertMany(ctx, orderItemsToBeInserted)

		if err != nil {
			log.Fatal(err)
		}

		defer cancel()

		c.JSON(http.StatusOK, insertedOrderItems)
	}
}

func UpdateOrderItem() gin.HandlerFunc {
	return func(c *gin.Context) {
		var ctx, cancel = context.WithTimeout(context.Background(), 100*time.Second)

		var orderItem models.OrderItem

		orderItemId := c.Param("order_item_id")

		filter := bson.M{"order_item_id": orderItemId}

		var updateObject primitive.D

		if orderItem.Unit_price != nil {
			updateObject = append(updateObject, bson.E{Key: "unit_price", Value: *&orderItem.Unit_price})
		}

		if orderItem.Quantity != nil {
			updateObject = append(updateObject, bson.E{Key: "quantity", Value: *orderItem.Quantity})
		}

		if orderItem.Food_id != nil {
			updateObject = append(updateObject, bson.E{Key: "food_id", Value: *orderItem.Food_id})
		}

		orderItem.Updated_at, _ = time.Parse(time.RFC3339, time.Now().Format(time.RFC3339))

		updateObject = append(updateObject, bson.E{Key: "updated_at", Value: orderItem.Updated_at})

		upsert := true

		opt := options.UpdateOptions{
			Upsert: &upsert,
		}

		result, err := orderItemCollection.UpdateOne(
			ctx,
			filter,
			bson.D{
				{Key: "$set", Value: updateObject},
			},
			&opt,
		)

		if err != nil {
			msg := "Order item update failed"
			c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
			return
		}

		defer cancel()
		c.JSON(http.StatusOK, result)

	}
}
